package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"sync"
)

// //////////////////////////////////////////////////////////////////////////
// Output
// //////////////////////////////////////////////////////////////////////////

// os/exec.Cmd.StdoutPipe is usually used incorrectly. The docs are clear:
// "it is incorrect to call Wait before all reads from the pipe have completed."
// Therefore, we can't read from the pipe in another goroutine because it
// causes a race condition: we'll read in one goroutine and the original
// goroutine that calls Wait will write on close which is what Wait does.
// The proper solution is using an io.Writer for cmd.Stdout. I couldn't find
// an io.Writer that's also safe for concurrent reads (as lines in a []string
// no less), so I created one:

// OutputBuffer represents command output that is saved, line by line, in an
// unbounded buffer. It is safe for multiple goroutines to read while the command
// is running and after it has finished. If output is small (a few megabytes)
// and not read frequently, an output buffer is a good solution.
//
// A Cmd in this package uses an OutputBuffer for both STDOUT and STDERR by
// default when created by calling NewCmd. To use OutputBuffer directly with
// a Go standard library os/exec.Command:
//
//   import "os/exec"
//   import "github.com/go-cmd/cmd"
//   runnableCmd := exec.Command(...)
//   stdout := cmd.NewOutputBuffer()
//   runnableCmd.Stdout = stdout
//
// While runnableCmd is running, call stdout.Lines() to read all output
// currently written.
type OutputBuffer struct {
	buf   *bytes.Buffer
	lines []string
	*sync.Mutex
}

// NewOutputBuffer creates a new output buffer. The buffer is unbounded and safe
// for multiple goroutines to read while the command is running by calling Lines.
func NewOutputBuffer() *OutputBuffer {
	out := &OutputBuffer{
		buf:   &bytes.Buffer{},
		lines: []string{},
		Mutex: &sync.Mutex{},
	}
	return out
}

// Write makes OutputBuffer implement the io.Writer interface. Do not call
// this function directly.
func (rw *OutputBuffer) Write(p []byte) (n int, err error) {
	rw.Lock()
	n, err = rw.buf.Write(p) // and bytes.Buffer implements io.Writer
	rw.Unlock()
	return // implicit
}

// Lines returns lines of output written by the Cmd. It is safe to call while
// the Cmd is running and after it has finished. Subsequent calls returns more
// lines, if more lines were written. "\r\n" are stripped from the lines.
func (rw *OutputBuffer) Lines() []string {
	rw.Lock()
	// Scanners are io.Readers which effectively destroy the buffer by reading
	// to EOF. So once we scan the buf to lines, the buf is empty again.
	s := bufio.NewScanner(rw.buf)
	for s.Scan() {
		rw.lines = append(rw.lines, s.Text())
	}
	rw.Unlock()
	return rw.lines
}

// --------------------------------------------------------------------------

const (
	// DEFAULT_LINE_BUFFER_SIZE is the default size of the OutputStream line buffer.
	// The default value is usually sufficient, but if ErrLineBufferOverflow errors
	// occur, try increasing the size by calling OutputBuffer.SetLineBufferSize.
	DEFAULT_LINE_BUFFER_SIZE = 16384

	// DEFAULT_STREAM_CHAN_SIZE is the default string channel size for a Cmd when
	// Options.Streaming is true. The string channel size can have a minor
	// performance impact if too small by causing OutputStream.Write to block
	// excessively.
	DEFAULT_STREAM_CHAN_SIZE = 1000
)

// ErrLineBufferOverflow is returned by OutputStream.Write when the internal
// line buffer is filled before a newline character is written to terminate a
// line. Increasing the line buffer size by calling OutputStream.SetLineBufferSize
// can help prevent this error.
type ErrLineBufferOverflow struct {
	Line       string // Unterminated line that caused the error
	BufferSize int    // Internal line buffer size
	BufferFree int    // Free bytes in line buffer
}

func (e ErrLineBufferOverflow) Error() string {
	return fmt.Sprintf("line does not contain newline and is %d bytes too long to buffer (buffer size: %d)",
		len(e.Line)-e.BufferSize, e.BufferSize)
}
