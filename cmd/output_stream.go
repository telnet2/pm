package cmd

import (
	"bytes"
)

// OutputStream represents real time, line by line output from a running Cmd.
// Lines are terminated by a single newline preceded by an optional carriage
// return. Both newline and carriage return are stripped from the line when
// sent to a caller-provided channel.
//
// The caller must begin receiving before starting the Cmd. Write blocks on the
// channel; the caller must always read the channel. The channel is closed when
// the Cmd exits and all output has been sent.
//
// A Cmd in this package uses an OutputStream for both STDOUT and STDERR when
// created by calling NewCmdOptions and Options.Streaming is true. To use
// OutputStream directly with a Go standard library os/exec.Command:
//
//   import "os/exec"
//   import "github.com/go-cmd/cmd"
//
//   stdoutChan := make(chan string, 100)
//   go func() {
//       for line := range stdoutChan {
//           // Do something with the line
//       }
//   }()
//
//   runnableCmd := exec.Command(...)
//   stdout := cmd.NewOutputStream(stdoutChan)
//   runnableCmd.Stdout = stdout
//
//
// While runnableCmd is running, lines are sent to the channel as soon as they
// are written and newline-terminated by the command.
type OutputStream struct {
	streamChan chan string
	bufSize    int
	buf        []byte
	lastChar   int
}

// NewOutputStream creates a new streaming output on the given channel. The
// caller must begin receiving on the channel before the command is started.
// The OutputStream never closes the channel.
func NewOutputStream(streamChan chan string) *OutputStream {
	out := &OutputStream{
		streamChan: streamChan,
		// --
		bufSize:  DEFAULT_LINE_BUFFER_SIZE,
		buf:      make([]byte, DEFAULT_LINE_BUFFER_SIZE),
		lastChar: 0,
	}
	return out
}

// Write makes OutputStream implement the io.Writer interface. Do not call
// this function directly.
func (rw *OutputStream) Write(p []byte) (n int, err error) {
	n = len(p) // end of buffer
	firstChar := 0

	for {
		// Find next newline in stream buffer. nextLine starts at 0, but buff
		// can contain multiple lines, like "foo\nbar". So in that case nextLine
		// will be 0 ("foo\nbar\n") then 4 ("bar\n") on next iteration. And i
		// will be 3 and 7, respectively. So lines are [0:3] are [4:7].
		newlineOffset := bytes.IndexByte(p[firstChar:], '\n')
		if newlineOffset < 0 {
			break // no newline in stream, next line incomplete
		}

		// End of line offset is start (nextLine) + newline offset. Like bufio.Scanner,
		// we allow \r\n but strip the \r too by decrementing the offset for that byte.
		lastChar := firstChar + newlineOffset // "line\n"
		if newlineOffset > 0 && p[newlineOffset-1] == '\r' {
			lastChar -= 1 // "line\r\n"
		}

		// Send the line, prepend line buffer if set
		var line string
		if rw.lastChar > 0 {
			line = string(rw.buf[0:rw.lastChar])
			rw.lastChar = 0 // reset buffer
		}
		line += string(p[firstChar:lastChar])
		rw.streamChan <- line // blocks if chan full

		// Next line offset is the first byte (+1) after the newline (i)
		firstChar += newlineOffset + 1
	}

	if firstChar < n {
		remain := len(p[firstChar:])
		bufFree := len(rw.buf[rw.lastChar:])
		if remain > bufFree {
			var line string
			if rw.lastChar > 0 {
				line = string(rw.buf[0:rw.lastChar])
			}
			line += string(p[firstChar:])
			err = ErrLineBufferOverflow{
				Line:       line,
				BufferSize: rw.bufSize,
				BufferFree: bufFree,
			}
			n = firstChar
			return // implicit
		}
		copy(rw.buf[rw.lastChar:], p[firstChar:])
		rw.lastChar += remain
	}

	return // implicit
}

// Lines returns the channel to which lines are sent. This is the same channel
// passed to NewOutputStream.
func (rw *OutputStream) Lines() <-chan string {
	return rw.streamChan
}

// SetLineBufferSize sets the internal line buffer size. The default is DEFAULT_LINE_BUFFER_SIZE.
// This function must be called immediately after NewOutputStream, and it is not
// safe to call by multiple goroutines.
//
// Increasing the line buffer size can help reduce ErrLineBufferOverflow errors.
func (rw *OutputStream) SetLineBufferSize(n int) {
	rw.bufSize = n
	rw.buf = make([]byte, rw.bufSize)
}

// Flush empties the buffer of its last line.
func (rw *OutputStream) Flush() {
	if rw.lastChar > 0 {
		line := string(rw.buf[0:rw.lastChar])
		rw.streamChan <- line
	}
}
