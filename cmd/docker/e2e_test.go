//go:build windows

package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const image = "alpine"

// buildShim builds docker.exe, or skips the test when WSL has no Docker.
func buildShim(t *testing.T) string {
	t.Helper()
	if err := exec.Command("wsl.exe", "-e", "docker", "info").Run(); err != nil {
		t.Skip("no Docker in the default WSL distribution")
	}
	exe := filepath.Join(t.TempDir(), "docker.exe")
	if out, err := exec.Command("go", "build", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return exe
}

func TestEndToEnd(t *testing.T) {
	docker := buildShim(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello world.txt"), []byte("from windows"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, args ...string) string {
		t.Helper()
		cmd := exec.Command(docker, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "SHIM_TEST=a b $c")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("docker %q: %v\n%s", args, err, out)
		}
		return strings.TrimRight(string(out), "\r\n")
	}
	expect := func(t *testing.T, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}

	t.Run("arguments arrive unchanged", func(t *testing.T) {
		weird := []string{"two words", `quo"te`, `back\slash`, `trail\`, "$HOME", "*", "", "a;b&c|d", "%PATH%", "{{.x}}", `C:\a b`}
		got := run(t, append([]string{"run", "--rm", image, "printf", `[%s]`}, weird...)...)
		expect(t, got, "["+strings.Join(weird, "][")+"]")
	})
	t.Run("volume", func(t *testing.T) {
		expect(t, run(t, "run", "--rm", "-v", dir+":/data:ro", image, "cat", "/data/hello world.txt"), "from windows")
	})
	t.Run("mount", func(t *testing.T) {
		expect(t, run(t, "run", "--rm", "--mount", "type=bind,src="+dir+",dst=/data", image, "cat", "/data/hello world.txt"), "from windows")
	})
	t.Run("relative volume uses the working directory", func(t *testing.T) {
		expect(t, run(t, "run", "--rm", "-v", `.\:/data`, image, "cat", "/data/hello world.txt"), "from windows")
	})
	t.Run("environment variable by name", func(t *testing.T) {
		expect(t, run(t, "run", "--rm", "-e", "SHIM_TEST", image, "printenv", "SHIM_TEST"), "a b $c")
	})
	t.Run("cidfile", func(t *testing.T) {
		cid := filepath.Join(dir, "cid.txt")
		run(t, "run", "--rm", "--cidfile", cid, image, "true")
		if raw, err := os.ReadFile(cid); err != nil || len(raw) != 64 {
			t.Errorf("cidfile: %q %v", raw, err)
		}
	})
	t.Run("build and compose", func(t *testing.T) {
		compose := "services:\n  app:\n    build:\n      context: .\n    image: docker-wsl-shim-test\n    environment: [\"V=${SHIM_TEST}\"]\n    volumes: [\".:/data\"]\n"
		files := map[string]string{"compose.yml": compose, "Dockerfile": "FROM " + image + "\nCOPY [\"hello world.txt\", \"/copied.txt\"]\n"}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() { _ = exec.Command(docker, "rmi", "-f", "docker-wsl-shim-test").Run() })
		run(t, "build", "-q", "-t", "docker-wsl-shim-test", "-f", filepath.Join(dir, "Dockerfile"), dir)
		expect(t, run(t, "run", "--rm", "docker-wsl-shim-test", "cat", "/copied.txt"), "from windows")
		file := filepath.Join(dir, "compose.yml")
		t.Cleanup(func() { _ = exec.Command(docker, "compose", "-p", "docker-wsl-shim-test", "-f", file, "down").Run() })
		got := run(t, "compose", "--progress", "quiet", "-p", "docker-wsl-shim-test", "-f", file, "run", "--rm", "-T", "app", "sh", "-c", "echo $V; cat '/data/hello world.txt'")
		if !strings.HasSuffix(got, "a b $c\nfrom windows") {
			t.Errorf("got %q", got)
		}
	})
	t.Run("exit code and separate streams", func(t *testing.T) {
		cmd := exec.Command(docker, "run", "--rm", image, "sh", "-c", "echo out; echo err >&2; exit 37")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		_ = cmd.Run()
		if code := cmd.ProcessState.ExitCode(); code != 37 || stdout.String() != "out\n" || stderr.String() != "err\n" {
			t.Errorf("code %d, stdout %q, stderr %q", code, &stdout, &stderr)
		}
	})
	t.Run("without a console, as from a GUI", func(t *testing.T) {
		cmd := exec.Command(docker, "run", "--rm", "-i", image, "sh", "-c", "cat; exit 5")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS}
		cmd.Stdin = strings.NewReader("in")
		out, _ := cmd.Output()
		if code := cmd.ProcessState.ExitCode(); code != 5 || string(out) != "in" {
			t.Errorf("code %d, stdout %q", code, out)
		}
	})
	t.Run("stdin, binary safe, ends on EOF", func(t *testing.T) {
		data := make([]byte, 1<<20)
		for i := range data {
			data[i] = byte(i * 7)
		}
		cmd := exec.Command(docker, "run", "--rm", "-i", image, "cat")
		cmd.Stdin = bytes.NewReader(data)
		out, err := cmd.Output()
		if err != nil || !bytes.Equal(out, data) {
			t.Errorf("%d bytes came back, err %v", len(out), err)
		}
	})
}

// Killing docker.exe must end the client in WSL too, as it would with the real one.
func TestKilledShimEndsTheClient(t *testing.T) {
	docker := buildShim(t)
	const name = "docker-wsl-shim-test-kill"
	t.Cleanup(func() { _ = exec.Command(docker, "rm", "-f", name).Run() })
	cmd := exec.Command(docker, "run", "--rm", "-i", "--name", name, image, "cat")
	stdin, err := cmd.StdinPipe() // held open: only the kill can end the container
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	cmd.Stdout = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	running := func() bool {
		out, _ := exec.Command(docker, "ps", "-q", "--filter", "name="+name).Output()
		return len(bytes.TrimSpace(out)) > 0
	}
	wait := func(want bool) bool {
		for range 40 {
			if running() == want {
				return true
			}
			time.Sleep(250 * time.Millisecond)
		}
		return false
	}
	if !wait(true) {
		t.Fatal("the container did not start")
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if !wait(false) {
		t.Error("the container outlived the killed docker.exe")
	}
}
