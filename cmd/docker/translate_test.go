package main

import (
	"slices"
	"strings"
	"testing"
)

func TestTranslateArgs(t *testing.T) {
	tests := []struct {
		name     string
		in, want []string
	}{
		{"nothing to do", []string{"ps", "-a", "--format", `{{.ID}}\t{{.Names}}`}, []string{"ps", "-a", "--format", `{{.ID}}\t{{.Names}}`}},
		{"volume", []string{"run", "-v", `C:\a b:/data:ro`, "img"}, []string{"run", "-v", "/mnt/c/a b:/data:ro", "img"}},
		{"volume, forward slashes", []string{"run", "-v", "D:/a:/data", "img"}, []string{"run", "-v", "/mnt/d/a:/data", "img"}},
		{"volume, inline", []string{"run", `--volume=C:\a:/data`, `-vC:\b:/b`, "img"}, []string{"run", "--volume=/mnt/c/a:/data", "-v/mnt/c/b:/b", "img"}},
		{"volume, flag cluster", []string{"run", "-itv", `C:\a:/data`, "img"}, []string{"run", "-itv", "/mnt/c/a:/data", "img"}},
		{"volume, relative", []string{"run", "-v", `.\data:/data`, "img"}, []string{"run", "-v", "./data:/data", "img"}},
		{"volume, named and Linux", []string{"run", "-v", "cache:/c", "-v", "/srv:/srv", "img"}, []string{"run", "-v", "cache:/c", "-v", "/srv:/srv", "img"}},
		{"volume, WSL share", []string{"run", "-v", `\\wsl.localhost\Ubuntu\home\me:/data`, "img"}, []string{"run", "-v", "/home/me:/data", "img"}},
		{"mount", []string{"run", "--mount", `type=bind,source=C:\a,target=/data`, "img"}, []string{"run", "--mount", "type=bind,source=/mnt/c/a,target=/data", "img"}},
		{"cidfile", []string{"run", "--cidfile", `C:\t\cid.txt`, "img"}, []string{"run", "--cidfile", "/mnt/c/t/cid.txt", "img"}},

		{"container command is left alone", []string{"run", "--rm", "-i", "img", "tool", "-v", `C:\a:/data`, `C:\b`}, []string{"run", "--rm", "-i", "img", "tool", "-v", `C:\a:/data`, `C:\b`}},
		{"exec command is left alone", []string{"exec", "-it", "-w", "/x", "c1", "cat", `C:\b`}, []string{"exec", "-it", "-w", "/x", "c1", "cat", `C:\b`}},
		{"container create", []string{"container", "create", "--name", "n", "-v", `C:\a:/d`, "img", `C:\b`}, []string{"container", "create", "--name", "n", "-v", "/mnt/c/a:/d", "img", `C:\b`}},
		{"env values are left alone", []string{"run", "-e", `P=C:\a`, "--label", `C:\l`, "img"}, []string{"run", "-e", `P=C:\a`, "--label", `C:\l`, "img"}},
		{"global options first", []string{"--context", "run", "run", "img", `C:\b`}, []string{"--context", "run", "run", "img", `C:\b`}},

		{"build", []string{"build", "-f", `C:\p\Dockerfile`, "-t", "x:y", "--build-arg", `A=C:\a`, "--secret", `id=s,src=C:\s.txt`, `C:\p`}, []string{"build", "-f", "/mnt/c/p/Dockerfile", "-t", "x:y", "--build-arg", `A=C:\a`, "--secret", "id=s,src=/mnt/c/s.txt", "/mnt/c/p"}},
		{"build, relative context", []string{"build", "-f", `.\docker\Dockerfile`, "."}, []string{"build", "-f", "./docker/Dockerfile", "."}},
		{"build output", []string{"buildx", "build", "-o", `type=local,dest=C:\out`, "."}, []string{"buildx", "build", "-o", "type=local,dest=/mnt/c/out", "."}},
		{"cp", []string{"cp", `C:\a.txt`, "c1:/b"}, []string{"cp", "/mnt/c/a.txt", "c1:/b"}},
		{"load and save", []string{"load", "-i", `C:\a.tar`}, []string{"load", "-i", "/mnt/c/a.tar"}},
		{"logs -f is not a file", []string{"logs", "-f", "c1"}, []string{"logs", "-f", "c1"}},

		{"compose file", []string{"compose", "-f", `C:\p\compose.yml`, "up", "-d"}, []string{"compose", "-f", "/mnt/c/p/compose.yml", "up", "-d"}},
		{"compose run", []string{"compose", "-f", `C:\p\c.yml`, "run", "--rm", "-v", `C:\a:/d`, "svc", "ls", `C:\b`}, []string{"compose", "-f", "/mnt/c/p/c.yml", "run", "--rm", "-v", "/mnt/c/a:/d", "svc", "ls", `C:\b`}},
		{"volume create does not run anything", []string{"volume", "create", "-o", `device=C:\a`, "v"}, []string{"volume", "create", "-o", "device=/mnt/c/a", "v"}},
		{"dashdash", []string{"run", "--", "img", `C:\b`}, []string{"run", "--", "img", `C:\b`}},
		{"no arguments", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := slices.Clone(tt.in)
			if got := translateArgs(in); !slices.Equal(got, tt.want) {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
			if !slices.Equal(in, tt.in) {
				t.Errorf("the input was changed: %q", in)
			}
		})
	}
}

func TestForwardEnv(t *testing.T) {
	out := forwardEnv([]string{
		"FOO=bar", "Path=C:\\Windows", "HOME=C:\\Users\\me", "ProgramFiles(x86)=C:\\x", "=C:=C:\\",
		"DOCKER_HOST=npipe:////./pipe/docker_engine", `DOCKER_CONFIG=C:\cfg`, `COMPOSE_FILE=C:\a.yml;C:\b.yml`,
		"LD_PRELOAD=x", "MINE=1", "WSLENV=MINE/p:OTHER",
	})
	var wslenv string
	for _, entry := range out {
		if value, ok := strings.CutPrefix(entry, "WSLENV="); ok {
			wslenv = value
		}
	}
	if want := "MINE/p:OTHER:FOO/u:DOCKER_CONFIG/up:COMPOSE_FILE/ul"; wslenv != want {
		t.Errorf("WSLENV\ngot  %s\nwant %s", wslenv, want)
	}
	if !slices.Contains(out, "FOO=bar") || !slices.Contains(out, "Path=C:\\Windows") {
		t.Errorf("wsl.exe itself must keep the whole environment: %q", out)
	}
	if host := forwardEnv([]string{"DOCKER_HOST=tcp://1.2.3.4:2375"}); !slices.Contains(host, "WSLENV=DOCKER_HOST/u") {
		t.Errorf("a reachable DOCKER_HOST is forwarded: %q", host)
	}
}
