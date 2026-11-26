package main

import (
	"path/filepath"
	"regexp"
	"strings"
)

// keptOutEnv would break the Linux side, or means something else there.
var keptOutEnv = set(
	"HOME", "HOSTNAME", "LOGNAME", "OLDPWD", "PATH", "PATHEXT", "PS1", "PWD", "SHELL", "SHLVL", "TEMP",
	"TERM", "TMP", "TMPDIR", "USER", "WSLENV", "WSL_UTF8", "_",
	activeEnv, dockerPathEnv, printCommandEnv,
)

// pathEnv hold paths, which WSL translates when asked to: /p for one path, /l for a list.
var pathEnv = map[string]string{
	"BUILDX_CONFIG":    "/p",
	"COMPOSE_FILE":     "/l",
	"DOCKER_CERT_PATH": "/p",
	"DOCKER_CONFIG":    "/p",
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// forwardEnv returns env plus a WSLENV that makes WSL pass it on. docker needs the caller's
// variables for `-e NAME`, for ${NAME} in compose files and for its own settings.
func forwardEnv(env []string) []string {
	var out, forward []string
	taken := map[string]bool{}
	for _, entry := range env {
		name, value, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, "WSLENV") {
			for _, item := range strings.Split(value, ":") {
				if item != "" {
					taken[strings.Split(item, "/")[0]] = true
					forward = append(forward, item)
				}
			}
			continue
		}
		out = append(out, entry)
	}
	for i, entry := range out {
		name, value, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		switch {
		case !envName.MatchString(name), keptOutEnv[upper], taken[name], strings.HasPrefix(upper, "LD_"):
		case upper == "DOCKER_HOST" && strings.HasPrefix(value, "npipe:"): // Docker Desktop's, unreachable from WSL
		default:
			if flag, ok := pathEnv[upper]; ok {
				out[i] = name + "=" + absPaths(value, flag)
			}
			forward = append(forward, name+"/u"+strings.TrimPrefix(pathEnv[upper], "/"))
		}
	}
	// Without a flag, WSL passes a variable both ways.
	forward = append(forward, activeEnv)
	// WSL_UTF8 makes wsl.exe print its own errors in UTF-8.
	return append(out, activeEnv+"="+clientMark, "WSL_UTF8=1", "WSLENV="+strings.Join(forward, ":"))
}

// absPaths makes relative paths absolute, as WSL drops what it cannot translate. docker would
// resolve them against the working directory, so this does too.
func absPaths(value, flag string) string {
	entries := []string{value}
	if flag == "/l" {
		entries = strings.Split(value, ";")
	}
	for i, entry := range entries {
		if entry != "" && !filepath.IsAbs(entry) {
			if abs, err := filepath.Abs(entry); err == nil {
				entries[i] = abs
			}
		}
	}
	return strings.Join(entries, ";")
}
