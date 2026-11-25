//go:build !windows

package main

// killWithParent exists so that the tests of the other files run on Linux too.
func killWithParent(int) error { return nil }
