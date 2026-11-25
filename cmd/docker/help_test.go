package main

import (
	"errors"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// fakeHelp has the layout of docker's own help, with options that options.txt doesn't have.
func fakeHelp(...string) (string, error) {
	return `
Options:
      --new new-type                     Takes a value, and its description
                                         --continues on the next line
  -N, --new-letter stringArray           Takes a value
      --optional string[="x"]            Takes an optional value
  -Z, --zero                             Takes no value (default
                                         "x")
      --last
`, nil
}

func TestParseHelp(t *testing.T) {
	help, _ := fakeHelp()
	got := parseHelp(help)
	want := options{"--new": true, "-N": true, "--new-letter": true, "--optional": false, "-Z": false, "--zero": false, "--last": false}
	if !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// The help costs another call to WSL, so it is read only for options that options.txt doesn't have.
func TestHelpIsReadOnlyWhenNeeded(t *testing.T) {
	for _, tt := range []struct {
		name  string
		args  []string
		calls []string
	}{
		{"no path", []string{"run", "--new", "x", "img", "curl", "https://x/y"}, nil},
		{"runs nothing in a container", []string{"build", "--new", "x", "-f", `C:\Dockerfile`, "."}, nil},
		{"known options", []string{"-D", "container", "run", "--rm", "-it", "-v", `C:\a:/d`, "--net", "host", "img"}, nil},
		{"known compose options", []string{"compose", "-f", `C:\c.yml`, "-p", "x", "run", "--rm", "-v", `C:\a:/d`, "svc"}, nil},
		{"value in the same argument", []string{"run", "--new=x", "-v", `C:\a:/d`, "img"}, nil},
		{"each command once", []string{"--new", "x", "compose", "--new", "x", "run", "--new", "x", "--optional", "-v", `C:\a:/d`, "svc"}, []string{"", "compose", "compose run"}},
		{"unknown letter in a cluster", []string{"run", "-iZv", `C:\a:/d`, "img"}, []string{"run"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			_, err := translateArgs(tt.args, func(command ...string) (string, error) {
				calls = append(calls, strings.Join(command, " "))
				return fakeHelp(command...)
			})
			if err != nil || !slices.Equal(calls, tt.calls) {
				t.Errorf("calls %q %v, want %q", calls, err, tt.calls)
			}
		})
	}
	_, err := translateArgs([]string{"run", "--new", "x", "-v", `C:\a:/d`, "img"}, func(...string) (string, error) {
		return "", errors.New("boom")
	})
	if err == nil || !strings.Contains(err.Error(), `"docker run"`) {
		t.Errorf("a failing help must not be ignored: %v", err)
	}
}

// Runs against the docker CLI in the image docker:$DOCKER_VERSION. CI sets it.
func TestHelpOfDockerVersion(t *testing.T) {
	version := os.Getenv("DOCKER_VERSION")
	if version == "" {
		t.Skip("DOCKER_VERSION is not set")
	}
	docker := []string{"docker"}
	if cli := os.Getenv("DOCKER_CLI"); cli != "" {
		docker = strings.Fields(cli)
	}
	help := func(command ...string) (string, error) {
		args := append([]string{"run", "--rm", "--entrypoint", "docker", "docker:" + version}, command...)
		args = append(slices.Clone(docker[1:]), append(args, "--help")...)
		out, err := exec.Command(docker[0], args...).Output()
		return string(out), err
	}
	// A wrong entry in options.txt breaks this version. A missing one costs it a call to WSL.
	for key, known := range knownOptions() {
		out, err := help(strings.Fields(key)...)
		if usage := strings.TrimSpace("docker " + key); err != nil || !strings.Contains(out, usage+" [") {
			t.Logf("docker %s has no %q", version, usage)
			continue
		}
		for name, takesValue := range parseHelp(out) {
			if want, ok := known[name]; !ok {
				t.Logf("not in options.txt: [%s] %s (takes a value: %v)", key, name, takesValue)
			} else if want != takesValue {
				t.Errorf("[%s] %s: takes a value: %v, options.txt says %v", key, name, takesValue, want)
			}
		}
	}
	for _, command := range [][]string{{"run"}, {"create"}, {"container", "run"}} {
		build := func(cfg, mount, volume string) []string {
			return slices.Concat([]string{"--config", cfg, "-D"}, command, []string{
				"--rm", "-it", "--init", "--privileged", "--name", "n", "--blkio-weight", "10", "-e", `K=C:\k`,
				"--mount", "type=bind,src=" + mount + ",dst=/m", "-v", volume + ":/d", "img", "-v", `C:\b:/d`,
			})
		}
		want := build("/mnt/c/cfg", "/mnt/c/m", "/mnt/c/a")
		got, err := translateArgs(build(`C:\cfg`, `C:\m`, `C:\a`), help)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%v\ngot  %q %v\nwant %q", command, got, err, want)
		}
	}
}
