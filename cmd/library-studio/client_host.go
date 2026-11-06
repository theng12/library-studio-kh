package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"librarystudio/internal/config"
)

// clientHostOutput is the local CLI contract for switching this desktop
// between its own server and another Library host webpage.
type clientHostOutput struct {
	OK         bool   `json:"ok"`
	ClientHost string `json:"clientHost"`
	Error      string `json:"error,omitempty"`
}

// runClientHost changes only this desktop's saved host. It never contacts a
// host and never starts or stops a local service.
func runClientHost(value, home string) (clientHostOutput, int) {
	if value == "clear" {
		if err := config.ClearClientHost(home); err != nil {
			return clientHostOutput{Error: err.Error()}, 1
		}
		return clientHostOutput{OK: true}, 0
	}
	host, err := config.SaveClientHost(home, value)
	if err != nil {
		return clientHostOutput{Error: err.Error()}, 1
	}
	return clientHostOutput{OK: true, ClientHost: host}, 0
}

func printClientHost(out clientHostOutput) {
	_ = json.NewEncoder(os.Stdout).Encode(out)
}

func remoteListeningLine(host string) string {
	return "LISTENING " + host
}

// remoteShell reports a saved client host before any local attach or start.
// A remote host is only a normal webpage URL: no local token is appended, and
// stdin still owns the helper's lifetime.
func remoteShell(home string, stdin io.Reader, stdout io.Writer) (bool, error) {
	host, err := config.LoadClientHost(home)
	if err != nil {
		return false, err
	}
	if host == "" {
		return false, nil
	}
	fmt.Fprintln(stdout, remoteListeningLine(host))
	waitShell(stdin)
	return true, nil
}
