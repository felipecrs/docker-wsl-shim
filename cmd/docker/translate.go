package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// pathFlags take a value that is, or contains, a path on the host.
var pathFlags = set(
	"--allow", "--build-context", "--buildkitd-config", "--cache-from", "--cache-to", "--cidfile",
	"--config", "--env-file", "--env-from-file", "--file", "--iidfile", "--input", "--label-file",
	"--metadata-file", "--mount", "--output", "--project-directory", "--secret", "--security-opt",
	"--set", "--ssh", "--tlscacert", "--tlscert", "--tlskey", "--volume",
	"-f", "-i", "-o", "-v",
)

// runCommands run something in a container: what follows the image or container is not docker's.
var runCommands = set(
	"run", "exec", "create",
	"container run", "container exec", "container create",
	"compose run", "compose exec",
)

// groups are commands whose subcommands matter for runCommands.
var groups = set("container", "compose")

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, item := range items {
		m[item] = true
	}
	return m
}

// translator rewrites the Windows paths in a docker command line to WSL paths.
type translator struct {
	args []string
	// help returns the output of `docker <command> --help`.
	help func(command ...string) (string, error)
	// valueOptions are the options that take a value, by command.
	valueOptions map[string]map[string]bool
}

// translateArgs rewrites the Windows paths in args to WSL paths and leaves what runs inside a
// container alone. help is called only when needed to tell the two apart.
func translateArgs(args []string, help func(command ...string) (string, error)) ([]string, error) {
	if !slices.ContainsFunc(args, mayHavePath) {
		return args, nil
	}
	t := translator{args: slices.Clone(args), help: help, valueOptions: map[string]map[string]bool{}}
	var command []string
	for i := 0; i < len(t.args); {
		if isFlag(t.args[i]) {
			next, err := t.flag(i, command)
			if err != nil {
				return nil, err
			}
			i = next
			continue
		}
		command = append(command, t.args[i])
		i++
		name := strings.Join(command, " ")
		if groups[name] {
			continue
		}
		if !runCommands[name] {
			t.rest(i)
			return t.args, nil
		}
		for i < len(t.args) && isFlag(t.args[i]) {
			next, err := t.flag(i, command)
			if err != nil {
				return nil, err
			}
			i = next
		}
		break // the image or container: what follows is not docker's
	}
	return t.args, nil
}

// flag rewrites the value of the flag at index i, if it has one, and returns the index after it.
func (t *translator) flag(i int, command []string) (next int, err error) {
	arg := t.args[i]
	takesValue, err := t.optionsWithValue(command)
	if err != nil {
		return 0, err
	}
	name, value, inline := splitFlag(arg, takesValue)
	switch {
	case inline:
		t.args[i] = strings.TrimSuffix(arg, value) + translateFlagValue(name, value)
	case takesValue[name] && i+1 < len(t.args):
		t.args[i+1] = translateFlagValue(name, t.args[i+1])
		return i + 2, nil
	}
	return i + 1, nil
}

// rest rewrites the arguments from start of a command that runs nothing in a container. Whether a
// flag takes a value makes no difference there.
func (t *translator) rest(start int) {
	for i := start; i < len(t.args); i++ {
		arg := t.args[i]
		if !isFlag(arg) {
			t.args[i] = translatePath(arg)
			continue
		}
		name, value, inline := splitFlag(arg, pathFlags)
		if inline {
			t.args[i] = strings.TrimSuffix(arg, value) + translateFlagValue(name, value)
		} else if i+1 < len(t.args) {
			t.args[i+1] = translateFlagValue(name, t.args[i+1])
		}
	}
}

// optionsWithValue learns the options of a docker command that take a value from its help, which
// keeps up with new docker versions.
func (t *translator) optionsWithValue(command []string) (map[string]bool, error) {
	key := strings.Join(command, " ")
	if options, ok := t.valueOptions[key]; ok {
		return options, nil
	}
	help, err := t.help(command...)
	if err != nil {
		return nil, fmt.Errorf("could not get the options of %q: %w", strings.TrimSpace("docker "+key), err)
	}
	t.valueOptions[key] = parseOptionsWithValue(help)
	return t.valueOptions[key], nil
}

// helpOption matches an option that takes a value: the option, one space, the value's type and at
// least two spaces. Without a value there is no type, and with an optional one it has a bracket.
var helpOption = regexp.MustCompile(`(?m)^ +(?:(-\w), )? *(--[\w-]+) [^\s\[]+ {2,}`)

func parseOptionsWithValue(help string) map[string]bool {
	options := map[string]bool{}
	for _, match := range helpOption.FindAllStringSubmatch(help, -1) {
		options[match[1]] = true
		options[match[2]] = true
	}
	delete(options, "")
	return options
}

// isFlag reports whether arg is a flag: "-" is a file name and "--" ends the flags.
func isFlag(arg string) bool {
	return strings.HasPrefix(arg, "-") && arg != "-" && arg != "--"
}

// splitFlag splits --flag=value and -fvalue. In a cluster such as -itv, the first letter that takes
// a value ends the flags.
func splitFlag(arg string, takesValue map[string]bool) (name, value string, inline bool) {
	if strings.HasPrefix(arg, "--") {
		return strings.Cut(arg, "=")
	}
	for i := 1; i < len(arg); i++ {
		name = "-" + arg[i:i+1]
		if takesValue[name] {
			value = strings.TrimPrefix(arg[i+1:], "=")
			return name, value, value != ""
		}
	}
	return name, "", false
}

// translateFlagValue rewrites the value of a flag that can hold a path: a path, a volume
// (C:\src:/dst:ro) or a list of key=value pairs (type=bind,src=C:\src,dst=/dst).
func translateFlagValue(flag, value string) string {
	if !pathFlags[flag] {
		return value
	}
	if end := volumeSource(value); end > 0 {
		return translatePath(value[:end]) + value[end:]
	}
	if isWindowsPath(value) {
		return translatePath(value)
	}
	fields := strings.Split(value, ",")
	for i, field := range fields {
		key, path, ok := strings.Cut(field, "=")
		// buildx bake --set target.args.NAME=value: the value is the build's, not docker's.
		if ok && (flag != "--set" || !strings.Contains(key, ".args.") && !strings.Contains(key, ".labels.")) {
			fields[i] = key + "=" + translatePath(path)
		}
	}
	return strings.Join(fields, ",")
}

// volumeSource returns where the source of a src:dst volume ends, or 0 if value is not one.
func volumeSource(value string) int {
	from := 0
	if drivePath.MatchString(value) {
		from = 2
	}
	if i := strings.Index(value[from:], ":/"); i > 0 && isWindowsPath(value[:from+i]) {
		return from + i
	}
	return 0
}

var (
	drivePath    = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
	relativePath = regexp.MustCompile(`^\.\.?\\`)
	wslShare     = regexp.MustCompile(`(?i)^[\\/]{2}(?:wsl\$|wsl\.localhost)[\\/][^\\/]+`)
	forwardDrive = regexp.MustCompile(`(?:^|[=,])[A-Za-z]:/[^/]|^-\w+:/[^/]`)
)

// mayHavePath is a cheap check for what translatePath could rewrite somewhere in arg.
func mayHavePath(arg string) bool {
	return strings.Contains(arg, `\`) || forwardDrive.MatchString(arg) || strings.Contains(arg, "//wsl")
}

func isWindowsPath(s string) bool {
	return drivePath.MatchString(s) || relativePath.MatchString(s) || wslShare.MatchString(s)
}

// translatePath rewrites a Windows path and returns anything else unchanged.
//
// shortcut: drives are expected under /mnt; ask wslpath if [automount] root in wsl.conf must work.
// shortcut: \\wsl.localhost\<distro> is taken for the default distribution.
func translatePath(p string) string {
	switch {
	case drivePath.MatchString(p):
		p = "/mnt/" + strings.ToLower(p[:1]) + p[2:]
	case wslShare.MatchString(p):
		if p = wslShare.ReplaceAllString(p, ""); p == "" {
			p = "/"
		}
	case !relativePath.MatchString(p):
		return p
	}
	return strings.ReplaceAll(p, `\`, "/")
}
