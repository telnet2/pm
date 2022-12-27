//go:build !windows
// +build !windows

package cmd_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/go-test/deep"
	"github.com/stretchr/testify/assert"
	"github.com/telnet2/pm/cmd"
)

func TestCmdOK(t *testing.T) {
	now := time.Now().Unix()

	p := cmd.NewCmd("echo", "echo foo")
	gotStatus := <-p.Start()
	expectStatus := cmd.Status{
		Name:     "echo",
		Cmd:      "echo foo",
		PID:      gotStatus.PID, // nondeterministic
		Complete: true,
		Exit:     0,
		Error:    nil,
		Runtime:  gotStatus.Runtime, // nondeterministic
		Stdout:   []string{"foo"},
		Stderr:   []string{},
	}
	if gotStatus.StartTs < now {
		t.Error("StartTs < now")
	}
	if gotStatus.StopTs < gotStatus.StartTs {
		t.Error("StopTs < StartTs")
	}
	gotStatus.StartTs = 0
	gotStatus.StopTs = 0
	if diffs := deep.Equal(gotStatus, expectStatus); diffs != nil {
		t.Error(diffs)
	}
	if gotStatus.PID < 0 {
		t.Errorf("got PID %d, expected non-zero", gotStatus.PID)
	}
	if gotStatus.Runtime < 0 {
		t.Errorf("got runtime %f, expected non-zero", gotStatus.Runtime)
	}
}

func TestCmdClone(t *testing.T) {
	opt := cmd.Options{
		Buffered: true,
	}
	c1 := cmd.NewCmdOptions(opt, "ls", "ls")
	c1.Dir = "/tmp/"
	c1.Env = []string{"YES=please"}
	c2 := c1.Clone()

	if c1.Name != c2.Name {
		t.Errorf("got Name %s, expecting %s", c2.Name, c1.Name)
	}
	if c1.Dir != c2.Dir {
		t.Errorf("got Dir %s, expecting %s", c2.Dir, c1.Dir)
	}
	if diffs := deep.Equal(c1.Env, c2.Env); diffs != nil {
		t.Error(diffs)
	}
}

func TestCmdNonzeroExit(t *testing.T) {
	p := cmd.NewCmd("false", "false")
	gotStatus := <-p.Start()
	expectStatus := cmd.Status{
		Name:     "false",
		Cmd:      "false",
		PID:      gotStatus.PID, // nondeterministic
		Complete: true,
		Exit:     1,
		Error:    nil,
		Runtime:  gotStatus.Runtime, // nondeterministic
		Stdout:   []string{},
		Stderr:   []string{},
	}
	gotStatus.StartTs = 0
	gotStatus.StopTs = 0
	if diffs := deep.Equal(gotStatus, expectStatus); diffs != nil {
		t.Error(diffs)
	}
	if gotStatus.PID < 0 {
		t.Errorf("got PID %d, expected non-zero", gotStatus.PID)
	}
	if gotStatus.Runtime < 0 {
		t.Errorf("got runtime %f, expected non-zero", gotStatus.Runtime)
	}
}

func TestCmdStop(t *testing.T) {
	// Count to 3 sleeping 5s between counts. The long sleep is because we want
	// to kill the proc right after count "1" to ensure Stdout only contains "1"
	// and also to ensure that the proc is really killed instantly because if
	// it's not then timeout below will trigger.
	p := cmd.NewCmd("count-and-sleep", "./test/count-and-sleep 3 5")

	// Start process in bg and get chan to receive final Status when done
	statusChan := p.Start()

	// Give it a second
	time.Sleep(1 * time.Second)

	// Kill the process
	err := p.Stop()
	if err != nil {
		t.Error(err)
	}

	// The final status should be returned instantly
	timeout := time.After(1 * time.Second)
	var gotStatus cmd.Status
	select {
	case gotStatus = <-statusChan:
	case <-timeout:
		t.Fatal("timeout waiting for statusChan")
	}

	start := time.Unix(0, gotStatus.StartTs)
	stop := time.Unix(0, gotStatus.StopTs)
	d := stop.Sub(start).Seconds()
	if d < 0.90 || d > 2 {
		t.Errorf("stop - start time not between 0.9s and 2.0s: %s - %s = %f", stop, start, d)
	}
	gotStatus.StartTs = 0
	gotStatus.StopTs = 0

	expectStatus := cmd.Status{
		Name:     "count-and-sleep",
		Cmd:      "./test/count-and-sleep 3 5",
		PID:      gotStatus.PID,                    // nondeterministic
		Complete: false,                            // signaled by Stop
		Exit:     -1,                               // signaled by Stop
		Error:    errors.New("signal: terminated"), // signaled by Stop
		Runtime:  gotStatus.Runtime,                // nondeterministic
		Stdout:   []string{"1"},
		Stderr:   []string{},
	}
	if diffs := deep.Equal(gotStatus, expectStatus); diffs != nil {
		t.Error(diffs)
	}
	if gotStatus.PID < 0 {
		t.Errorf("got PID %d, expected non-zero", gotStatus.PID)
	}
	if gotStatus.Runtime < 0 {
		t.Errorf("got runtime %f, expected non-zero", gotStatus.Runtime)
	}

	// Stop should be idempotent
	err = p.Stop()
	if err != nil {
		t.Error(err)
	}

	// Start should be idempotent, too. It just returns the same statusChan again.
	c2 := p.Start()
	if diffs := deep.Equal(statusChan, c2); diffs != nil {
		t.Error(diffs)
	}
}

func TestCmdNotStarted(t *testing.T) {
	// Call everything _but_ Start.
	p := cmd.NewCmd("echo", "foo")

	gotStatus := p.Status()
	expectStatus := cmd.Status{
		Name:     "echo",
		Cmd:      "foo",
		PID:      0,
		Complete: false,
		Exit:     -1,
		Error:    nil,
		Runtime:  0,
		Stdout:   nil,
		Stderr:   nil,
	}
	if diffs := deep.Equal(gotStatus, expectStatus); diffs != nil {
		t.Error(diffs)
	}

	err := p.Stop()
	if err != cmd.ErrNotStarted {
		t.Error(err)
	}
}

func TestCmdOutput(t *testing.T) {
	tmpfile, err := ioutil.TempFile("", "cmd.TestCmdOutput")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("temp file: %s", tmpfile.Name())
	os.Remove(tmpfile.Name())

	p := cmd.NewCmd("touch-file-count", fmt.Sprintf("./test/touch-file-count %s", tmpfile.Name()))

	p.Start()

	touchFile := func(file string) {
		if err := exec.Command("touch", file).Run(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(600 * time.Millisecond)
	}
	var s cmd.Status
	var stdout []string

	touchFile(tmpfile.Name())
	s = p.Status()
	stdout = []string{"1"}
	if diffs := deep.Equal(s.Stdout, stdout); diffs != nil {
		t.Log(s.Stdout)
		t.Error(diffs)
	}

	touchFile(tmpfile.Name())
	s = p.Status()
	stdout = []string{"1", "2"}
	if diffs := deep.Equal(s.Stdout, stdout); diffs != nil {
		t.Log(s.Stdout)
		t.Error(diffs)
	}

	// No more output yet
	s = p.Status()
	stdout = []string{"1", "2"}
	if diffs := deep.Equal(s.Stdout, stdout); diffs != nil {
		t.Log(s.Stdout)
		t.Error(diffs)
	}

	// +2 lines
	touchFile(tmpfile.Name())
	touchFile(tmpfile.Name())
	s = p.Status()
	stdout = []string{"1", "2", "3", "4"}
	if diffs := deep.Equal(s.Stdout, stdout); diffs != nil {
		t.Log(s.Stdout)
		t.Error(diffs)
	}

	// Kill the process
	if err := p.Stop(); err != nil {
		t.Error(err)
	}
}

func TestCmdNotFound(t *testing.T) {
	p := cmd.NewCmd("cmd-does-not-exist", "cmd-does-not-exist")
	gotStatus := <-p.Start()
	gotStatus.StartTs = 0
	gotStatus.StopTs = 0
	expectStatus := cmd.Status{
		Name:     "cmd-does-not-exist",
		Cmd:      "cmd-does-not-exist",
		PID:      gotStatus.PID,
		Complete: true,
		Exit:     127,
		// Error:    &exec.Error{Name: "cmd-does-not-exist", Err: errors.New(`executable file not found in $PATH`)},
		Runtime: gotStatus.Runtime,
		Stdout:  []string{},
		Stderr:  []string{fmt.Sprintf("%s: line 1: cmd-does-not-exist: command not found", p.Shell)},
	}
	if diffs := deep.Equal(gotStatus, expectStatus); diffs != nil {
		if !strings.Contains(diffs[0], "Stderr.slice[0]") {
			t.Logf("%+v", gotStatus)
			t.Error(diffs)
		}
	}
}

func TestCmdLost(t *testing.T) {
	// Test something like the kernel OOM killing the proc. So the proc is
	// stopped outside our control.
	p := cmd.NewCmd("count-and-sleep", "./test/count-and-sleep 3 5")

	statusChan := p.Start()

	// Give it a second
	time.Sleep(1 * time.Second)

	// Get the PID and kill it
	s := p.Status()
	if s.PID <= 0 {
		t.Fatalf("got PID %d, expected PID > 0", s.PID)
	}
	pgid, err := syscall.Getpgid(s.PID)
	if err != nil {
		t.Fatal(err)
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL) // -pid = process group of pid

	// Even though killed externally, our wait should return instantly
	timeout := time.After(1 * time.Second)
	var gotStatus cmd.Status
	select {
	case gotStatus = <-statusChan:
	case <-timeout:
		t.Fatal("timeout waiting for statusChan")
	}
	gotStatus.Runtime = 0 // nondeterministic
	gotStatus.StartTs = 0
	gotStatus.StopTs = 0

	expectStatus := cmd.Status{
		Name:     "count-and-sleep",
		Cmd:      "./test/count-and-sleep 3 5",
		PID:      s.PID,
		Complete: false,
		Exit:     -1,
		Error:    errors.New("signal: killed"),
		Runtime:  0,
		Stdout:   []string{"1"},
		Stderr:   []string{},
	}
	if diffs := deep.Equal(gotStatus, expectStatus); diffs != nil {
		t.Logf("%+v\n", gotStatus)
		t.Error(diffs)
	}
}

func TestDone(t *testing.T) {
	// Count to 3 sleeping 1s between counts
	p := cmd.NewCmd("count-and-sleep", "./test/count-and-sleep 3 1")
	statusChan := p.Start()

	// For 2s while cmd is running, Done() chan should block, which means
	// it's still running
	runningTimer := time.After(2 * time.Second)
TIMER:
	for {
		select {
		case <-runningTimer:
			break TIMER
		default:
		}
		select {
		case <-p.Done():
			t.Fatal("Done chan is closed before runningTime finished")
		default:
			// Done chan blocked, cmd is still running
		}
		time.Sleep(400 * time.Millisecond)
	}

	// Wait for cmd to complete
	var s1 cmd.Status
	select {
	case s1 = <-statusChan:
		t.Logf("got status: %+v", s1)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for cmd to complete")
	}

	// After cmd completes, Done chan should be closed and not block
	select {
	case <-p.Done():
	default:
		t.Fatal("Done chan did not block after cmd completed")
	}

	// After command completes, we should be able to get exact same
	// Status that's returned on the Start() chan
	s2 := p.Status()
	if diff := deep.Equal(s1, s2); diff != nil {
		t.Error(diff)
	}
}

func TestCmdEnvOK(t *testing.T) {
	now := time.Now().Unix()

	p := cmd.NewCmd("env", "env")
	p.Env = []string{"FOO=foo"}
	gotStatus := <-p.Start()
	expectStatus := cmd.Status{
		Name:     "env",
		Cmd:      "env",
		PID:      gotStatus.PID, // nondeterministic
		Complete: true,
		Exit:     0,
		Error:    nil,
		Runtime:  gotStatus.Runtime, // nondeterministic
		Stdout:   gotStatus.Stdout,
		Stderr:   []string{},
	}
	if gotStatus.StartTs < now {
		t.Error("StartTs < now")
	}
	if gotStatus.StopTs < gotStatus.StartTs {
		t.Error("StopTs < StartTs")
	}
	assert.Contains(t, gotStatus.Stdout, "FOO=foo")

	gotStatus.StartTs = 0
	gotStatus.StopTs = 0
	if diffs := deep.Equal(gotStatus, expectStatus); diffs != nil {
		t.Error(diffs)
	}
	if gotStatus.PID < 0 {
		t.Errorf("got PID %d, expected non-zero", gotStatus.PID)
	}
	if gotStatus.Runtime < 0 {
		t.Errorf("got runtime %f, expected non-zero", gotStatus.Runtime)
	}
}

func TestCmdNoOutput(t *testing.T) {
	// Set both output options to false to discard all output
	p := cmd.NewCmdOptions(
		cmd.Options{
			Buffered: false,
		},
		"echo", "hell-world")
	s := <-p.Start()
	if s.Exit != 127 {
		t.Errorf("got exit %d, expected 127", s.Exit)
	}
	if len(s.Stdout) != 0 {
		t.Errorf("got stdout, expected no output: %v", s.Stdout)
	}
	if len(s.Stderr) != 0 {
		t.Errorf("got stderr, expected no output: %v", s.Stderr)
	}
}

func TestCmdStopMultiple(t *testing.T) {
	// Set both output options to false to discard all output
	p := cmd.NewCmdOptions(
		cmd.Options{
			Buffered:  true,
			Streaming: true,
		},
		"loop", "for i in {0..100}; do echo $i; sleep 0.1; done")
	p.Start()
	stdout := p.StdoutPub.Subscribe()
	for log := range stdout {
		fmt.Println(log)
		if log == "10" {
			_ = p.Stop()
		}
	}
	assert.NoError(t, p.Stop())
	assert.NoError(t, p.Stop())
	assert.NoError(t, p.Stop())
}

func TestStdinOk(t *testing.T) {
	tests := []struct {
		in []byte
	}{
		{in: []byte("1")},
		{in: []byte("hello")},
		{in: []byte{65, 66, 67, 226, 130, 172}}, // ABC€

	}
	for _, tt := range tests {
		now := time.Now().Unix()
		p := cmd.NewCmd("stdin", "test/stdin")
		gotStatus := <-p.StartWithStdin(bytes.NewReader(tt.in))
		expectStatus := cmd.Status{
			Name:     "stdin",
			Cmd:      "test/stdin",
			PID:      gotStatus.PID, // nondeterministic
			Complete: true,
			Exit:     0,
			Error:    nil,
			Runtime:  gotStatus.Runtime, // nondeterministic
			Stdout:   []string{"stdin: " + string(tt.in)},
			Stderr:   []string{},
		}
		if gotStatus.StartTs < now {
			t.Error("StartTs < now")
		}
		if gotStatus.StopTs < gotStatus.StartTs {
			t.Error("StopTs < StartTs")
		}
		gotStatus.StartTs = 0
		gotStatus.StopTs = 0
		if diffs := deep.Equal(gotStatus, expectStatus); diffs != nil {
			t.Error(diffs)
		}
		if gotStatus.PID < 0 {
			t.Errorf("got PID %d, expected non-zero", gotStatus.PID)
		}
		if gotStatus.Runtime < 0 {
			t.Errorf("got runtime %f, expected non-zero", gotStatus.Runtime)
		}
	}
}

func TestOptionsBeforeExec(t *testing.T) {
	handled := false
	p := cmd.NewCmdOptions(
		cmd.Options{
			BeforeExec: []func(cmd *exec.Cmd){
				func(cmd *exec.Cmd) { handled = true },
			},
		},
		"ls",
		"/bin/ls",
	)
	<-p.Start()
	if !handled {
		t.Error("exec cmd option not applied")
	}

	// nil funcs should be ignored, not cause a panic
	handled = false
	p = cmd.NewCmdOptions(
		cmd.Options{
			BeforeExec: []func(cmd *exec.Cmd){
				nil,
				func(cmd *exec.Cmd) { handled = true },
			},
		},
		"ls",
		"/bin/ls",
	)
	<-p.Start()
	if !handled {
		t.Error("exec cmd option not applied")
	}

	// Cloning should copy the funcs
	handled = false
	p2 := p.Clone()
	<-p2.Start()
	if !handled {
		t.Error("exec cmd option not applied")
	}
}
