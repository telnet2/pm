package term

import (
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// ExecSSH executes an SSH session for the current session.
// `args` is the argument list for the `ssh` process created.
func ExecSSH(args []string) error {
	// Create arbitrary command.
	c := exec.Command("ssh", args...)
	c.Env = os.Environ()

	// Start the command with a pty.
	ptyMaster, err := pty.Start(c)
	if err != nil {
		return err
	}
	// Make sure to close the pty at the end.
	defer func() { _ = ptyMaster.Close() }() // Best effort.

	// Handle pty size.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			_ = pty.InheritSize(os.Stdin, ptyMaster)
		}
	}()

	ch <- syscall.SIGWINCH                        // Initial resize.
	defer func() { signal.Stop(ch); close(ch) }() // Cleanup signals when done.

	// Set stdin in raw mode.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }() // Best effort.

	// Copy stdin to the pty and the pty to stdout.
	// NOTE: The goroutine will keep reading until the next keystroke before returning.
	go func() { _, _ = io.Copy(ptyMaster, os.Stdin) }()
	_, _ = io.Copy(os.Stdout, ptyMaster)

	return nil
}
