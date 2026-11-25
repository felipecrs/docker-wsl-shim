package main

import (
	"regexp"
	"strings"
)

// pathFlags take a value that is, or contains, a path on the host.
var pathFlags = set(
	"--build-context", "--cache-from", "--cache-to", "--cidfile", "--config", "--env-file", "--file",
	"--iidfile", "--input", "--label-file", "--metadata-file", "--mount", "--output",
	"--project-directory", "--secret", "--security-opt", "--ssh", "--tlscacert", "--tlscert",
	"--tlskey", "--volume",
	"-f", "-i", "-o", "-v",
)

// globalValueFlags come before the command and take a value: those of docker, then of compose.
var globalValueFlags = set(
	"--config", "--context", "--host", "--log-level", "--tlscacert", "--tlscert", "--tlskey",
	"-H", "-c", "-l",
	"--ansi", "--env-file", "--file", "--parallel", "--profile", "--progress", "--project-directory",
	"--project-name",
	"-f", "-p",
)

// runBoolFlags are the flags of run, create and exec (docker's and compose's) that take no value.
// Every other flag does, which is how the end of the flags is found.
var runBoolFlags = set(
	"--build", "--detach", "--disable-content-trust", "--help", "--init", "--interactive", "--no-deps",
	"--no-healthcheck", "--no-TTY", "--no-tty", "--oom-kill-disable", "--privileged", "--publish-all",
	"--quiet", "--quiet-build", "--quiet-pull", "--read-only", "--remove-orphans", "--rm",
	"--service-ports", "--sig-proxy", "--tty", "--use-aliases", "--use-api-socket",
	"-P", "-T", "-d", "-i", "-q", "-t",
)

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, item := range items {
		m[item] = true
	}
	return m
}

// translateArgs rewrites the Windows paths in a docker command line to WSL paths. What runs inside
// a container is left alone: everything after the image or container of run, create and exec.
func translateArgs(args []string) []string {
	out := append([]string(nil), args...)
	i, runs := findCommand(out)
	if !runs {
		translateFlags(out, 0, func(string) bool { return false })
		return out
	}
	translateFlags(out[:i], 0, func(string) bool { return false })
	translateFlags(out, i, func(flag string) bool { return !runBoolFlags[flag] })
	return out
}

// findCommand returns the index after the command's words, and whether the command runs something
// in a container.
func findCommand(args []string) (next int, runs bool) {
	group := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			if globalValueFlags[arg] {
				i++
			}
			continue
		}
		switch {
		case group == "" && (arg == "container" || arg == "compose"):
			group = arg
			continue
		case arg == "run" || arg == "exec":
			return i + 1, true
		case arg == "create":
			return i + 1, group != "compose"
		}
		return i + 1, false
	}
	return len(args), false
}

// translateFlags rewrites args[start:] in place. While takesValue is true for a flag, the argument
// after it is its value. It stops at the first positional argument if takesValue ever says so,
// because what follows then belongs to the container.
func translateFlags(args []string, start int, takesValue func(flag string) bool) {
	stopAtPositional := takesValue("--any-unknown-flag")
	for i := start; i < len(args); i++ {
		arg := args[i]
		if arg == "--" || !strings.HasPrefix(arg, "-") || arg == "-" {
			if stopAtPositional {
				return
			}
			args[i] = translatePath(arg)
			continue
		}
		flag, value, inline := splitFlag(arg)
		if inline {
			if pathFlags[flag] {
				args[i] = arg[:len(arg)-len(value)] + translateValue(value)
			}
			continue
		}
		if i+1 < len(args) && (pathFlags[flag] || takesValue(flag)) {
			if pathFlags[flag] {
				args[i+1] = translateValue(args[i+1])
			}
			if takesValue(flag) {
				i++
			}
		}
	}
}

// splitFlag splits --flag=value and -fvalue. In a cluster such as -itv, the last flag decides.
func splitFlag(arg string) (flag, value string, inline bool) {
	if strings.HasPrefix(arg, "--") {
		flag, value, inline = strings.Cut(arg, "=")
		return flag, value, inline
	}
	for i := 1; i < len(arg); i++ {
		flag = "-" + arg[i:i+1]
		if rest := arg[i+1:]; rest != "" && !runBoolFlags[flag] {
			return flag, strings.TrimPrefix(rest, "="), true
		}
	}
	return flag, "", false
}

// translateValue rewrites a path flag's value: a path, a volume (C:\src:/dst:ro) or a list of
// key=value pairs (type=bind,src=C:\src,dst=/dst).
func translateValue(value string) string {
	if driveEnd := volumeSource(value); driveEnd > 0 {
		return translatePath(value[:driveEnd]) + value[driveEnd:]
	}
	if isWindowsPath(value) || !strings.Contains(value, "=") {
		return translatePath(value)
	}
	fields := strings.Split(value, ",")
	for i, field := range fields {
		if key, path, ok := strings.Cut(field, "="); ok {
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
)

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
