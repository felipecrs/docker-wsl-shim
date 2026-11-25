//go:build !windows

package main

import "os/exec"

// These exist so that the tests of the other files run on Linux too.

func shield(*exec.Cmd) {}

func keepCtrlC() {}

func killWithParent(int) error { return nil }
