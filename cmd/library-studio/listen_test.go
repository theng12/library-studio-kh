package main

import (
	"errors"
	"net"
	"strings"
	"testing"
)

// A taken port must produce a plain message, never a panic or a bare error.
func TestListenProblemExplainsAPortInUse(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	_, err = net.Listen("tcp", first.Addr().String())
	if err == nil {
		t.Fatal("a second listen on the same address succeeded")
	}
	message := listenProblem(first.Addr().String(), err)
	if !strings.Contains(message, "Another Library Studio KH is already running") {
		t.Fatalf("port-in-use message = %q", message)
	}
}

func TestListenProblemKeepsOtherReasons(t *testing.T) {
	message := listenProblem("127.0.0.1:0", errors.New("permission denied"))
	if !strings.Contains(message, "could not start its server") || !strings.Contains(message, "permission denied") {
		t.Fatalf("other message = %q", message)
	}
}
