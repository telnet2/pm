// Package cmd runs external commands with concurrent access to output and
// status. It wraps the Go standard library os/exec.Command to correctly handle
// reading output (STDOUT and STDERR) while a command is running and killing a
// command. All operations are safe to call from multiple goroutines.
//
// A basic example that runs env and prints its output:
//
//	import (
//	    "fmt"
//	    "github.com/go-cmd/cmd"
//	)
//
//	func main() {
//	    // Create Cmd, buffered output
//	    envCmd := cmd.NewCmd("env")
//
//	    // Run and wait for Cmd to return Status
//	    status := <-envCmd.Start()
//
//	    // Print each line of STDOUT from Cmd
//	    for _, line := range status.Stdout {
//	        fmt.Println(line)
//	    }
//	}
//
// Commands can be ran synchronously (blocking) or asynchronously (non-blocking):
//
//	envCmd := cmd.NewCmd("env") // create
//
//	status := <-envCmd.Start() // run blocking
//
//	statusChan := envCmd.Start() // run non-blocking
//	// Do other work while Cmd is running...
//	status <- statusChan // blocking
//
// Start returns a channel to which the final Status is sent when the command
// finishes for any reason. The first example blocks receiving on the channel.
// The second example is non-blocking because it saves the channel and receives
// on it later. Only one final status is sent to the channel; use Done for
// multiple goroutines to wait for the command to finish, then call Status to
// get the final status.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/telnet2/pm/debug"
)

var (
	_log = debug.NewDebugLogger("pm:cmd")
	// _logOutput = debug.NewDebugLogger("pm:cmd:output")
)

// Cmd represents an external command, similar to the Go built-in os/exec.Cmd.
// A Cmd cannot be reused after calling Start. Exported fields are read-only and
// should not be modified, except Env which can be set before calling Start.
// To create a new Cmd, call NewCmd or NewCmdOptions.
type Cmd struct {
	// Name of binary (command) to run. This is the only required value.
	// No path expansion is done.
	// Used to set underlying os/exec.Cmd.Path.
	Name string

	// Commands line arguments passed to the command.
	// Args are optional.
	// Used to set underlying os/exec.Cmd.Args.
	Args []string

	// Environment variables set before running the command.
	// Env is optional.
	Env []string

	// Current working directory from which to run the command.
	// Dir is optional; default is current working directory
	// of calling process.
	// Used to set underlying os/exec.Cmd.Dir.
	Dir string

	// If set, log files are generated as `${LogDir}/${id}.{stdout,stderr}.log`.
	LogDir string

	// StdoutPub publishes the stdout to multiple subscribers and is created only when Streaming option is true.
	StdoutPub *StringPublisher

	// StderrPub publishes the stderr to multiple subscribers and is created only when Streaming option is true.
	StderrPub *StringPublisher

	Shell string // shell binary to execute

	*sync.Mutex
	started   bool      // cmd.Start called, no error
	stopped   bool      // Stop called
	done      bool      // run() done
	final     bool      // status finalized in Status
	startTime time.Time // if started true
	stdoutBuf *OutputBuffer
	stderrBuf *OutputBuffer
	// stdout sets streaming STDOUT if enabled, else nil (see Options).
	stdout chan string
	// stderr sets streaming STDERR if enabled, else nil (see Options).
	stderr          chan string
	stdoutStream    *OutputStream
	stderrStream    *OutputStream
	stdoutFile      *os.File
	stderrFile      *os.File
	status          Status
	statusChan      chan Status   // nil until Start() called
	doneChan        chan struct{} // closed when done running
	beforeExecFuncs []func(cmd *exec.Cmd)
	afterDoneFuncs  []func(cmd *Cmd)
}

var (
	// ErrNotStarted is returned by Stop if called before Start or StartWithStdin.
	ErrNotStarted = errors.New("command not running")
)

// Status represents the running status and consolidated return of a Cmd. It can
// be obtained any time by calling Cmd.Status. If StartTs > 0, the command has
// started. If StopTs > 0, the command has stopped. After the command finishes
// for any reason, this combination of values indicates success (presuming the
// command only exits zero on success):
//
//	Exit     = 0
//	Error    = nil
//	Complete = true
//
// Error is a Go error from the underlying os/exec.Cmd.Start or os/exec.Cmd.Wait.
// If not nil, the command either failed to start (it never ran) or it started
// but was terminated unexpectedly (probably signaled). In either case, the
// command failed. Callers should check Error first. If nil, then check Exit and
// Complete.
type Status struct {
	Name     string
	Cmd      string
	PID      int
	Complete bool     // false if stopped or signaled
	Exit     int      // exit code of process
	Error    error    // Go error
	StartTs  int64    // Unix ts (nanoseconds), zero if Cmd not started
	StopTs   int64    // Unix ts (nanoseconds), zero if Cmd not started or running
	Runtime  float64  // seconds, zero if Cmd not started
	Stdout   []string // buffered STDOUT; see Cmd.Status for more info
	Stderr   []string // buffered STDERR; see Cmd.Status for more info
}

// NewCmd creates a new Cmd for the given command name and arguments. The command
// is not started until Start is called. Output buffering is on, streaming output
// is off. To control output, use NewCmdOptions instead.
func NewCmd(name string, execmd string) *Cmd {
	return NewCmdOptions(Options{Buffered: true}, name, execmd)
}

// Options represents customizations for NewCmdOptions.
type Options struct {
	// If Buffered is true, STDOUT and STDERR are written to Status.Stdout and
	// Status.Stderr. The caller can call Cmd.Status to read output at intervals.
	// See Cmd.Status for more info.
	Buffered bool

	// If Streaming is true, Cmd.Stdout and Cmd.Stderr channels are created and
	// STDOUT and STDERR output lines are written them in real time. This is
	// faster and more efficient than polling Cmd.Status. The caller must read both
	// streaming channels, else lines are dropped silently.
	Streaming bool

	// BeforeExec is a list of functions called immediately before starting
	// the real command. These functions can be used to customize the underlying
	// os/exec.Cmd. For example, to set SysProcAttr.
	BeforeExec []func(cmd *exec.Cmd)

	// BeforeExec is a list of functions called immediately after the real command exits.
	AfterDone []func(cmd *Cmd)
}

// NewCmdOptions creates a new Cmd with options. The command is not started
// until Start is called.
func NewCmdOptions(options Options, name string, execmd string) *Cmd {
	c := &Cmd{
		Name: name,
		Args: []string{"-c", execmd},
		// --
		Mutex: &sync.Mutex{},
		status: Status{
			Name:     name,
			Cmd:      execmd,
			PID:      0,
			Complete: false,
			Exit:     -1,
			Error:    nil,
			Runtime:  0,
		},
		doneChan: make(chan struct{}),
	}

	if options.Buffered {
		c.stdoutBuf = NewOutputBuffer()
		c.stderrBuf = NewOutputBuffer()
	}

	if options.Streaming {
		c.stdout = make(chan string, DEFAULT_STREAM_CHAN_SIZE)
		c.stdoutStream = NewOutputStream(c.stdout)

		c.stderr = make(chan string, DEFAULT_STREAM_CHAN_SIZE)
		c.stderrStream = NewOutputStream(c.stderr)

		// wait for publishing to start
		wg := sync.WaitGroup{}
		wg.Add(2)

		c.StdoutPub = NewStringPublisher(time.Microsecond*100, 100)
		go func() {
			_log.Debugf("[%s] start publishing stdout", c.Name)
			wg.Done()
			// TODO: evaluate if we can directly use the publisher in the OutputBuffer.
			// c.Stdout will be closed when the cmd is done.
			for s := range c.stdout {
				c.StdoutPub.Publish(s)
			}
			// Close the publisher
			_log.Debugf("[%s] stop publishing stdout", c.Name)
			c.StdoutPub.Close()
		}()

		c.StderrPub = NewStringPublisher(time.Microsecond*100, 100)
		go func() {
			// TODO: evaluate if we can directly use the publisher in the OutputBuffer.
			// c.Stderr will be closed when the cmd is done.
			_log.Debugf("[%s] start publishing stderr", c.Name)
			wg.Done()
			for s := range c.stderr {
				c.StderrPub.Publish(s)
			}
			// Close the publisher
			_log.Debugf("[%s] stop publishing stderr", c.Name)
			c.StderrPub.Close()
		}()

		wg.Wait()
	}

	if len(options.BeforeExec) > 0 {
		c.beforeExecFuncs = []func(cmd *exec.Cmd){}
		for _, f := range options.BeforeExec {
			if f == nil {
				continue
			}
			c.beforeExecFuncs = append(c.beforeExecFuncs, f)
		}
	}

	if len(options.AfterDone) > 0 {
		c.afterDoneFuncs = []func(cmd *Cmd){}
		for _, f := range options.AfterDone {
			if f == nil {
				continue
			}
			c.afterDoneFuncs = append(c.afterDoneFuncs, f)
		}
	}

	if binBash, err := exec.LookPath("bash"); err != nil {
		if binSh, err := exec.LookPath("sh"); err != nil {
			// If no bash, no sh found, return nil
			return nil
		} else {
			c.Shell = binSh
		}
	} else {
		c.Shell = binBash
	}

	return c
}

// Clone clones a Cmd. All the options are transferred,
// but the internal state of the original object is lost.
// Cmd is one-use only, so if you need to re-start a Cmd,
// you need to Clone it.
func (c *Cmd) Clone() *Cmd {
	clone := NewCmdOptions(
		Options{
			Buffered:  c.stdoutBuf != nil,
			Streaming: c.stdoutStream != nil,
		},
		c.Name,
		c.Args[1], // Assume that c.Args = []string{"-c", execmd}
	)
	clone.Dir = c.Dir
	clone.Env = c.Env
	clone.LogDir = c.LogDir

	if len(c.beforeExecFuncs) > 0 {
		clone.beforeExecFuncs = make([]func(cmd *exec.Cmd), len(c.beforeExecFuncs))
		for i := range c.beforeExecFuncs {
			clone.beforeExecFuncs[i] = c.beforeExecFuncs[i]
		}
	}

	if len(c.afterDoneFuncs) > 0 {
		clone.afterDoneFuncs = make([]func(cmd *Cmd), len(c.afterDoneFuncs))
		for i := range c.afterDoneFuncs {
			clone.afterDoneFuncs[i] = c.afterDoneFuncs[i]
		}
	}

	return clone
}

// Start starts the command and immediately returns a channel that the caller
// can use to receive the final Status of the command when it ends. The caller
// can start the command and wait like,
//
//	status := <-myCmd.Start() // blocking
//
// or start the command asynchronously and be notified later when it ends,
//
//	statusChan := myCmd.Start() // non-blocking
//	// Do other work while Cmd is running...
//	status := <-statusChan // blocking
//
// Exactly one Status is sent on the channel when the command ends. The channel
// is not closed. Any Go error is set to Status.Error. Start is idempotent; it
// always returns the same channel.
func (c *Cmd) Start() <-chan Status {
	return c.StartWithStdin(nil)
}

// IsDone returns true if the Cmd is done and exited; otherwise, return false
func (c *Cmd) IsDone() bool {
	select {
	case _, ok := <-c.doneChan:
		return !ok
	default:
		return false
	}
}

// StartWithStdin is the same as Start but uses in for STDIN.
func (c *Cmd) StartWithStdin(in io.Reader) <-chan Status {
	c.Lock()
	defer c.Unlock()

	if c.statusChan != nil {
		return c.statusChan
	}

	c.statusChan = make(chan Status, 1)
	go c.run(in)
	return c.statusChan
}

// Stop stops the command by sending its process group a SIGTERM signal.
// Stop is idempotent. Stopping and already stopped command returns nil.
//
// Stop returns ErrNotStarted if called before Start or StartWithStdin. If the
// command is very slow to start, Stop can return ErrNotStarted after calling
// Start or StartWithStdin because this package is still waiting for the system
// to start the process. All other return errors are from the low-level system
// function for process termination.
func (c *Cmd) Stop() error {
	c.Lock()
	defer c.Unlock()

	// c.statusChan is created in StartWithStdin()/Start(), so if nil the caller
	// hasn't started the command yet. c.started is set true in run() only after
	// the underlying os/exec.Cmd.Start() has returned without an error, so we're
	// sure the command has started (although it might exit immediately after,
	// we at least know it started).
	if c.statusChan == nil || !c.started {
		return ErrNotStarted
	}

	// c.done is set true as the very last thing run() does before returning.
	// If it's true, we're certain the command is completely done (regardless
	// of its exit status), so it can't be running.
	if c.done {
		return nil
	}

	// Flag that command was stopped, it didn't complete. This results in
	// status.Complete = false
	c.stopped = true

	// Signal the process group (-pid), not just the process, so that the process
	// and all its children are signaled. Else, child procs can keep running and
	// keep the stdout/stderr fd open and cause cmd.Wait to hang.
	return terminateProcess(c.status.PID)
}

// Status returns the Status of the command at any time. It is safe to call
// concurrently by multiple goroutines.
//
// With buffered output, Status.Stdout and Status.Stderr contain the full output
// as of the Status call time. For example, if the command counts to 3 and three
// calls are made between counts, Status.Stdout contains:
//
//	"1"
//	"1 2"
//	"1 2 3"
//
// The caller is responsible for tailing the buffered output if needed. Else,
// consider using streaming output. When the command finishes, buffered output
// is complete and final.
//
// Status.Runtime is updated while the command is running and final when it
// finishes.
func (c *Cmd) Status() Status {
	c.Lock()
	defer c.Unlock()

	// Return default status if cmd hasn't been started
	if c.statusChan == nil || !c.started {
		return c.status
	}

	if c.done {
		// No longer running
		if !c.final {
			if c.stdoutBuf != nil {
				c.status.Stdout = c.stdoutBuf.Lines()
				c.status.Stderr = c.stderrBuf.Lines()
				c.stdoutBuf = nil // release buffers
				c.stderrBuf = nil
			}
			c.final = true
		}
	} else {
		// Still running
		c.status.Runtime = time.Since(c.startTime).Seconds()
		if c.stdoutBuf != nil {
			c.status.Stdout = c.stdoutBuf.Lines()
			c.status.Stderr = c.stderrBuf.Lines()
		}
	}

	return c.status
}

// Done returns a channel that's closed when the command stops running.
// This method is useful for multiple goroutines to wait for the command
// to finish.Call Status after the command finishes to get its final status.
func (c *Cmd) Done() <-chan struct{} {
	return c.doneChan
}

// --------------------------------------------------------------------------

func (c *Cmd) run(in io.Reader) {
	defer func() {
		if c.stdoutFile != nil {
			_ = c.stdoutFile.Close()
		}
		if c.stderrFile != nil {
			_ = c.stderrFile.Close()
		}
		c.statusChan <- c.Status() // unblocks Start if caller is waiting
		close(c.doneChan)

		if c.status.Error != nil {
			_log.Infof("[%s] stopped with %v", c.Name, c.status.Error)
		} else {
			_log.Infof("[%s] stopped with %d", c.Name, c.status.Exit)
		}
		func() {
			defer func() { _ = recover() }()
			for _, f := range c.afterDoneFuncs {
				f(c)
			}
		}()
	}()

	// //////////////////////////////////////////////////////////////////////
	// Setup command
	// //////////////////////////////////////////////////////////////////////
	cmd := exec.Command(c.Shell, c.Args...)
	if in != nil {
		cmd.Stdin = in
	}

	// finisher helps to return with an error
	finisher := func(err error) {
		c.Lock()
		c.status.Error = err
		c.done = true
		c.Unlock()
	}

	// Platform-specific SysProcAttr management
	setProcessGroupID(cmd)

	if c.LogDir != "" {
		var err error
		c.stdoutFile, err = CreateLogFile(c.LogDir, c.Name, true)
		if err != nil {
			finisher(err)
			return
		}

		c.stderrFile, err = CreateLogFile(c.LogDir, c.Name, false)
		if err != nil {
			finisher(err)
			return
		}
	}

	// Set exec.Cmd.Stdout and .Stderr to our concurrent-safe stdout/stderr
	// buffer, stream both, or neither
	switch {
	case c.stdoutBuf != nil && c.stdoutStream != nil: // buffer and stream
		if c.stdoutFile != nil {
			cmd.Stdout = io.MultiWriter(c.stdoutFile, c.stdoutStream, c.stdoutBuf)
		} else {
			cmd.Stdout = io.MultiWriter(c.stdoutStream, c.stdoutBuf)
		}
		if c.stderrFile != nil {
			cmd.Stderr = io.MultiWriter(c.stderrFile, c.stderrStream, c.stderrBuf)
		} else {
			cmd.Stderr = io.MultiWriter(c.stderrStream, c.stderrBuf)
		}
	case c.stdoutBuf != nil: // buffer only
		if c.stdoutFile != nil {
			cmd.Stdout = io.MultiWriter(c.stdoutFile, c.stdoutBuf)
		} else {
			cmd.Stdout = c.stdoutBuf
		}
		if c.stdoutFile != nil {
			cmd.Stderr = io.MultiWriter(c.stdoutFile, c.stderrBuf)
		} else {
			cmd.Stderr = c.stderrBuf
		}
	case c.stdoutStream != nil: // stream only
		if c.stdoutFile != nil {
			cmd.Stdout = io.MultiWriter(c.stdoutFile, c.stdoutStream)
		} else {
			cmd.Stdout = c.stdoutStream
		}
		if c.stderrFile != nil {
			cmd.Stderr = io.MultiWriter(c.stderrFile, c.stderrStream)
		} else {
			cmd.Stderr = c.stderrStream
		}
	default: // no output (cmd >/dev/null 2>&1)
		if c.stdoutFile != nil {
			cmd.Stdout = c.stdoutFile
		} else {
			cmd.Stdout = nil
		}
		if c.stderrFile != nil {
			cmd.Stderr = c.stderrFile
		} else {
			cmd.Stderr = nil
		}
	}

	// Always close output streams. Do not do this after Wait because if Start
	// fails and we return without closing these, it could deadlock the caller
	// who's waiting for us to close them.
	if c.stdoutStream != nil {
		defer func() {
			c.stdoutStream.Flush()
			c.stderrStream.Flush()
			// exec.Cmd.Wait has already waited for all output:
			//   Otherwise, during the execution of the command a separate goroutine
			//   reads from the process over a pipe and delivers that data to the
			//   corresponding Writer. In this case, Wait does not complete until the
			//   goroutine reaches EOF or encounters an error.
			// from https://golang.org/pkg/os/exec/#Cmd
			close(c.stdout)
			close(c.stderr)
		}()
	}

	// Set the runtime environment for the command as per os/exec.Cmd.  If Env
	// is nil, use the current process' environment.
	cmd.Env = c.Env
	cmd.Dir = c.Dir

	// Run all optional commands to customize underlying os/exe.Cmd.
	for _, f := range c.beforeExecFuncs {
		f(cmd)
	}

	// //////////////////////////////////////////////////////////////////////
	// Start command
	// //////////////////////////////////////////////////////////////////////
	_log.Debugf("[%s] is starting", c.Name)
	if err := cmd.Start(); err != nil {
		finisher(err)
		return
	}
	_log.Debugf("[%s] is started", c.Name)

	// Set initial status
	now := time.Now()
	c.Lock()
	c.startTime = now              // command is running
	c.status.PID = cmd.Process.Pid // command is running
	c.status.StartTs = now.UnixNano()
	c.started = true
	c.Unlock()

	// //////////////////////////////////////////////////////////////////////
	// Wait for command to finish or be killed
	// //////////////////////////////////////////////////////////////////////
	err := cmd.Wait()
	now = time.Now()

	// Get exit code of the command. According to the manual, Wait() returns:
	// "If the command fails to run or doesn't complete successfully, the error
	// is of type *ExitError. Other error types may be returned for I/O problems."
	exitCode := 0
	signaled := false
	if err != nil && fmt.Sprintf("%T", err) == "*exec.ExitError" {
		// This is the normal case which is not really an error. It's string
		// representation is only "*exec.ExitError". It only means the cmd
		// did not exit zero and caller should see ExitError.Stderr, which
		// we already have. So first we'll have this as the real/underlying
		// type, then discard err so status.Error doesn't contain a useless
		// "*exec.ExitError". With the real type we can get the non-zero
		// exit code and determine if the process was signaled, which yields
		// a more specific error message, so we set err again in that case.
		exiterr := err.(*exec.ExitError)
		err = nil
		if waitStatus, ok := exiterr.Sys().(syscall.WaitStatus); ok {
			exitCode = waitStatus.ExitStatus() // -1 if signaled
			if waitStatus.Signaled() {
				signaled = true
				err = errors.New(exiterr.Error()) // "signal: terminated"
			}
		}
	}

	// Set final status
	c.Lock()
	if !c.stopped && !signaled {
		c.status.Complete = true
	}
	c.status.Runtime = now.Sub(c.startTime).Seconds()
	c.status.StopTs = now.UnixNano()
	c.status.Exit = exitCode
	c.status.Error = err
	c.done = true
	c.Unlock()
}
