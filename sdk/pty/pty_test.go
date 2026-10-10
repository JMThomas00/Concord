//go:build !windows

package pty_test

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/plugintest"
	"github.com/JMThomas00/Concord/sdk/pty"
	"github.com/google/uuid"
)

// TestHelperProgram is the program the tests run in a terminal: it greets,
// then echoes each line it reads.
func TestHelperProgram(t *testing.T) {
	if os.Getenv("PTY_HELPER") == "" {
		return
	}
	fmt.Println("\x1b[1mhello from the program\x1b[0m")
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		fmt.Printf("got: %s\n", in.Text())
	}
	os.Exit(0)
}

func TestProgramRunsInAChannel(t *testing.T) {
	exe, _ := os.Executable()
	srv := plugintest.NewServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go plugin.Run(ctx, srv.Config(), pty.Handler(pty.Options{
		Command: exe, Args: []string{"-test.run=^TestHelperProgram$"}, Env: []string{"PTY_HELPER=1"},
	}))
	srv.WaitReady()

	ch := uuid.New()
	alice := srv.Enter(ch, "alice", 60, 10)
	srv.FrameContaining(alice, "hello from the program")
	if e := srv.NextEvent(); e.Kind != "pane_title" {
		t.Fatalf("expected the pane title to name the driver, got %+v", e)
	}

	bob := srv.Enter(ch, "bob", 60, 10)
	srv.FrameContaining(bob, "hello from the program")
	srv.Type(bob, "no") // bob isn't driving: ignored
	srv.Type(alice, "hi")
	srv.Key(alice, "enter")
	f := srv.FrameContaining(bob, "got: hi")
	if strings.Contains(f, "got: no") || strings.Contains(f, "nohi") {
		t.Fatalf("a non-driver's keys reached the program:\n%s", f)
	}
}
