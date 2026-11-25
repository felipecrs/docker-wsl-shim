package main

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

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

// shield keeps a Ctrl+C or Ctrl+Break sent to this process and its group away from cmd.
func shield(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// keepCtrlC undoes what wsl.exe does to the console even if its input is something else: there,
// Ctrl+C becomes input that nothing reads.
func keepCtrlC() {
	var mode uint32
	if windows.GetConsoleMode(windows.Stdin, &mode) == nil {
		return // wsl.exe reads it and passes it on
	}
	console, err := windows.Open("CONIN$", windows.O_RDWR, 0)
	if err != nil {
		return
	}
	if windows.GetConsoleMode(console, &mode) != nil || mode&windows.ENABLE_PROCESSED_INPUT == 0 {
		return
	}
	go func() {
		for windows.GetConsoleMode(console, &mode) == nil {
			if mode&windows.ENABLE_PROCESSED_INPUT == 0 {
				_ = windows.SetConsoleMode(console, mode|windows.ENABLE_PROCESSED_INPUT)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
}
