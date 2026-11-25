//go:build windows

// Command docker runs the docker CLI of the default WSL distribution as if it were docker.exe.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	// -e (--exec) runs docker without a shell, so that every argument arrives unchanged.
	wsl := filepath.Join(os.Getenv("SystemRoot"), "System32", "wsl.exe")
	cmd := exec.Command(wsl, append([]string{"-e", "docker"}, translateArgs(args)...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = forwardEnv(os.Environ())

	// wsl.exe gets Ctrl+C from the console too. Stay alive to report docker's own exit code.
	signal.Notify(make(chan os.Signal, 1), os.Interrupt, syscall.SIGTERM)

	err := cmd.Start()
	if err == nil {
		if jobErr := killWithParent(cmd.Process.Pid); jobErr != nil {
			fmt.Fprintln(os.Stderr, "docker-wsl-shim: warning:", jobErr)
		}
		err = cmd.Wait()
	}
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	default:
		fmt.Fprintln(os.Stderr, "docker-wsl-shim:", err)
		return 127
	}
}

// killWithParent makes Windows end process pid when this process ends, however it ends. The job
// handle is left open on purpose: closing it is what ends pid.
func killWithParent(pid int) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return fmt.Errorf("set up job object: %w", err)
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open wsl.exe: %w", err)
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return fmt.Errorf("assign wsl.exe to job object: %w", err)
	}
	return nil
}
