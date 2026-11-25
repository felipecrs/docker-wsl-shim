package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// clientMark is the value of activeEnv, unique to this process to find its docker client in WSL.
var clientMark = fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())

// interruptScript waits for the client, which may still be starting. Its plugins have the mark too,
// and get the signal as they would from a terminal.
const interruptScript = `
i=0
while [ $i -lt 30 ]; do
	found=$(grep -ls "$1" /proc/[0-9]*/environ)
	if [ -n "$found" ]; then
		for file in $found; do
			file=${file#/proc/}
			kill -INT "${file%/environ}"
		done
		exit 0
	fi
	i=$((i + 1))
	sleep 0.1
done
exit 1
`

// interruptClient sends SIGINT to the docker client in WSL. wsl.exe would not: Ctrl+Break ends it,
// and so does Ctrl+C unless it reads from the console.
func interruptClient() error {
	cmd := exec.Command(wslPath(), "-e", "sh", "-c", interruptScript, "sh", activeEnv+"="+clientMark)
	shield(cmd)
	return cmd.Run()
}
