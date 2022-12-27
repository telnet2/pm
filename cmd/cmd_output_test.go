package cmd_test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/telnet2/pm/cmd"
)

func TestLogOutput(t *testing.T) {
	ch := make(chan string, 100)
	os := cmd.NewOutputStream(ch)

	w := sync.WaitGroup{}
	w.Add(1)

	go func() {
		for s := range ch {
			_ = s
		}
		w.Done()
	}()

	for i := 1; i <= 100; i++ {
		_, _ = os.Write([]byte(fmt.Sprintf("%03d\n", i)))
		// time.Sleep(time.Microsecond)
		// Can we wait until the remaining data is read?
	}

	os.Flush()
	close(ch)

	w.Wait()
}

func TestCmdOutputPub(t *testing.T) {
	c := cmd.NewCmdOptions(cmd.Options{Streaming: true},
		"count-and-sleep",
		"./test/count-and-sleep 10 0.1")

	ch := c.StdoutPub.Subscribe()

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		for s := range ch {
			_ = s
		}
		wg.Done()
	}()

	<-c.Start()
	wg.Wait()
}

func TestLogDir(t *testing.T) {
	c := cmd.NewCmdOptions(cmd.Options{Streaming: true},
		"echo", "./test/echo.sh")

	// create echo.stdout.log
	// create echo.stderr.log
	c.LogDir = "."
	<-c.Start()

	assert.FileExists(t, "echo.stdout.log")
	assert.FileExists(t, "echo.stderr.log")

	{
		stdout, err := ioutil.ReadFile("echo.stdout.log")
		assert.NoError(t, err)
		assert.Equal(t, "stdout\n", string(stdout))
	}

	{
		stderr, err := ioutil.ReadFile("echo.stderr.log")
		assert.NoError(t, err)
		assert.Equal(t, "stderr\n", string(stderr))
	}

	c2 := c.Clone()
	assert.Equal(t, c2.LogDir, ".")

	<-c2.Start()

	assert.FileExists(t, "echo.stdout.log.bak")
	assert.FileExists(t, "echo.stderr.log.bak")

	os.Remove("echo.stdout.log")
	os.Remove("echo.stderr.log")
	os.Remove("echo.stdout.log.bak")
	os.Remove("echo.stderr.log.bak")
}

func TestOnStop(t *testing.T) {
	w := sync.WaitGroup{}
	c := cmd.NewCmdOptions(cmd.Options{
		Streaming: true,
		AfterDone: []func(cmd *cmd.Cmd){func(cmd *cmd.Cmd) { w.Done() }},
	}, "echo", "./test/echo.sh")
	w.Add(1)
	_ = c.Start()
	w.Wait()
}

func TestOnStopPanic(t *testing.T) {
	w := sync.WaitGroup{}
	c := cmd.NewCmdOptions(cmd.Options{Streaming: true,
		AfterDone: []func(cmd *cmd.Cmd){func(cmd *cmd.Cmd) {
			w.Done()
			panic(errors.New("test"))
		}},
	}, "echo", "./test/echo.sh")
	w.Add(1)
	c.Start()
	w.Wait()
}
