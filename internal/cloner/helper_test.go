package cloner_test

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// helperEnv selects what TestHelperProcess does when the test binary runs
// itself as a child process.
const helperEnv = "GHX_CLONER_TEST_HELPER"

// exitInterrupted is the helper's exit code once it has seen os.Interrupt.
const exitInterrupted = 3

// TestHelperProcess is not a test: run as a child of another test, it stands
// in for a long git command that waits to be stopped.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "wait-for-interrupt" {
		t.Skip("only runs as a child process")
	}
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	fmt.Println("ready")
	select {
	case <-c:
		os.Exit(exitInterrupted)
	case <-time.After(time.Minute):
		os.Exit(4)
	}
}

// startWaiter starts TestHelperProcess as a child and returns once it is
// listening for os.Interrupt.
func startWaiter(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), helperEnv+"=wait-for-interrupt")
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "ready\n", line)
	return cmd
}
