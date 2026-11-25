// Command docker runs the docker CLI of the default WSL distribution as if it were docker.exe.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	// activeEnv marks a docker in WSL that the shim started. It travels back to Windows too.
	activeEnv = "DOCKER_WSL_SHIM_ACTIVE"
	// dockerPathEnv names the docker to run in WSL.
	dockerPathEnv = "DOCKER_WSL_SHIM_DOCKER_PATH"
	// printCommandEnv prints the command instead of running it.
	printCommandEnv = "DOCKER_WSL_SHIM_PRINT_COMMAND"
)

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR(docker-wsl-shim):", err)
	}
	os.Exit(code)
}

func run(args []string) (code int, err error) {
	if os.Getenv(activeEnv) != "" {
		return 1, errors.New("docker in WSL led back to this shim: install Docker in the default WSL distribution, or set " + dockerPathEnv)
	}
	docker := os.Getenv(dockerPathEnv)
	if docker == "" {
		docker = "docker"
	}
	args, err = translateArgs(args, func(command ...string) (string, error) {
		cmd := wsl(append(append([]string{docker}, command...), "--help")...)
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		return string(out), err
	})
	if err != nil {
		return 1, err
	}
	if os.Getenv(printCommandEnv) == "true" {
		fmt.Println(docker, strings.Join(args, " "))
		return 0, nil
	}

	// wsl.exe gets Ctrl+C from the console too. Stay alive to report docker's own exit code.
	signal.Notify(make(chan os.Signal, 1), os.Interrupt, syscall.SIGTERM)

	cmd := wsl(append([]string{docker}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return 127, err
	}
	if err := killWithParent(cmd.Process.Pid); err != nil {
		fmt.Fprintln(os.Stderr, "WARNING(docker-wsl-shim):", err)
	}
	err = cmd.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return 127, err
	}
	return 0, nil
}

// wsl returns a command that runs in the default WSL distribution. -e (--exec) uses no shell, so
// that every argument arrives unchanged.
func wsl(args ...string) *exec.Cmd {
	exe := filepath.Join(os.Getenv("SystemRoot"), "System32", "wsl.exe")
	if programs := os.Getenv("ProgramFiles"); programs != "" {
		// The one in System32 only starts this one, which takes time.
		if direct := filepath.Join(programs, "WSL", "wsl.exe"); exists(direct) {
			exe = direct
		}
	}
	cmd := exec.Command(exe, append([]string{"-e"}, args...)...)
	cmd.Env = forwardEnv(os.Environ())
	return cmd
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
