package main

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// fakeHelp has the layout of docker's own help.
func fakeHelp(command ...string) (string, error) {
	switch strings.Join(command, " ") {
	case "":
		return `
Options:
      --config string      Location of client config files (default
                           "/root/.docker")
  -c, --context string     Name of the context
  -D, --debug              Enable debug mode
`, nil
	case "compose":
		return `
Options:
      --all-resources              Include all resources
      --env-file stringArray       Specify an alternate environment file
  -f, --file stringArray           Compose configuration files
  -p, --project-name string        Project name
`, nil
	}
	return `
Options:
      --cidfile string                   Write the container ID to the file
  -d, --detach                           Run container in background
  -e, --env list                         Set environment variables
      --gpus gpu-request                 GPU devices to add to the container
  -i, --interactive                      Keep STDIN open even if not attached
      --label list                       Set meta data on a container
      --mount mount                      Attach a filesystem mount
      --name string                      Assign a name to the container
      --optional string[="x"]            Takes an optional value
      --rm                               Automatically remove the container
  -t, --tty                              Allocate a pseudo-TTY
  -v, --volume list                      Bind mount a volume
  -w, --workdir string                   Working directory inside the container
      --use-api-socket                   Bind mount Docker API socket and
                                         required auth
`, nil
}

// The help costs a call to WSL, so it is read only when a path could be on either side.
func TestHelpIsReadOnlyWhenNeeded(t *testing.T) {
	for _, tt := range []struct {
		name  string
		args  []string
		calls []string
	}{
		{"no path", []string{"run", "--rm", "-v", "cache:/c", "-v", "/srv:/srv", "img", "curl", "https://x/y"}, nil},
		{"runs nothing in a container", []string{"build", "-f", `C:\Dockerfile`, "."}, nil},
		{"no flag", []string{"run", "img", `C:\b`}, nil},
		{"each command once", []string{"-D", "compose", "-f", `C:\c.yml`, "-p", "x", "run", "--rm", "-v", `C:\a:/d`, "svc"}, []string{"", "compose", "compose run"}},
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
	_, err := translateArgs([]string{"run", "-v", `C:\a:/d`, "img"}, func(...string) (string, error) {
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
