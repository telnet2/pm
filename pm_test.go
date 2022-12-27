package pm_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/suite"
	"github.com/telnet2/pm"
)

type ProcManTester struct {
	suite.Suite
}

func (tm *ProcManTester) TestSimpleRunner() {
	pman := pm.NewProcMan()
	_ = pman.Add(&pm.Runnable{
		Id:      "ls",
		Command: "ls -al",
	})

	wg := sync.WaitGroup{}

	// Wait for log printing
	wg.Add(1)
	lsStdout := pman.SubscribeLog("ls", true)
	go func() {
		for l := range lsStdout {
			fmt.Println(l)
		}
		wg.Done()
	}()

	// Subscribe to the events of `ls`
	sch := pman.SubscribeEvent("ls", pm.EventMap{"start": {}, "done": {}})

	// Must subscribe before the process starting
	ctx := context.TODO()
	_ = pman.Start(ctx)

	log.Println("wait start")
	e := <-sch
	tm.Equal(e.Id, "ls")
	tm.Equal("start", e.Event)

	e = <-sch
	tm.Equal(e.Id, "ls")
	tm.Equal("done", e.Event)
	wg.Wait()

	tm.NoError(pman.Restart(ctx, "ls"))

	e = <-sch
	tm.Equal(e.Id, "ls")
	tm.Equal("start", e.Event)

	lsStdout = pman.SubscribeLog("ls", true)
	go func() {
		for l := range lsStdout {
			fmt.Println(l)
		}
	}()

	e = <-sch
	tm.Equal(e.Id, "ls")
	tm.Equal("done", e.Event)

	pman.Shutdown(nil)
}

func (tm *ProcManTester) TestSimpleConfig() {
	ctx := context.TODO()
	cfg, err := pm.LoadYaml(ctx, strings.NewReader(`---
services:
    first:
        command: "echo first > first.txt && echo done"
        ready_condition:
            stdout_message: done
    second:
        command: "[[ -f first.txt ]] && echo second > second.txt || echo error > error.txt; echo done"
        ready_condition:
            stdout_message: done
        depends_on:
            - first
    `))
	tm.NoError(err)
	tm.NotNil(cfg)

	procMan := pm.NewProcMan()
	tm.NoError(procMan.AddConfig(cfg))

	// Start will wait until the second prints 'done' due to ready_condition
	tm.NoError(procMan.Start(ctx))

	tm.FileExists("first.txt")
	tm.FileExists("second.txt")

	os.Remove("first.txt")
	os.Remove("second.txt")
	// should implement waitdone
	// procMan.WaitDone()
}

func (tm *ProcManTester) TestEchoGood() {
	ctx := context.TODO()
	pman := pm.NewProcMan()
	cfg, err := pm.LoadYamlFile(ctx, "./testdata/config_echo.yaml")
	tm.NoError(err)
	tm.NoError(pman.AddConfig(cfg))
	tm.NoError(pman.Start(ctx))

	cli := resty.New()
	res, err := cli.R().Get("http://localhost:41234/echo")
	tm.NoError(err)
	tm.Equal("/echo", res.Header().Get("x-request-path"))
	// echo_ready is already gone!
	tm.NoError(pman.Stop("echo"))

	pman.WaitDone()

	// check if the echo server is really stopped
	res, err = cli.R().Get("http://localhost:41234/echo")
	log.Println(string(res.Body()), err)
	log.Println(res.IsSuccess())
	tm.False(res.IsSuccess())
	tm.Error(err)
}

func (tm *ProcManTester) TestEchoFail() {
	ctx := context.TODO()
	pman := pm.NewProcMan()
	cfg, err := pm.LoadYamlFile(ctx, "./testdata/config_echo_fail.yaml")
	tm.NoError(err)
	tm.NoError(pman.AddConfig(cfg))
	// error after 5 secs because ready checker is listening a wrong port.
	tm.Error(pman.Start(ctx))
	pman.WaitDone()
}

func (tm *ProcManTester) TestMySQL() {
	ctx := context.TODO()
	pman := pm.NewProcMan()
	cfg, err := pm.LoadYamlFile(ctx, "./testdata/config_echo_fail.yaml")
	tm.NoError(err)
	tm.NoError(pman.AddConfig(cfg))
	// error after 5 secs because ready checker is listening a wrong port.
	tm.Error(pman.Start(ctx))
	pman.WaitDone()
}

func TestProcMan(t *testing.T) {
	tm := new(ProcManTester)
	suite.Run(t, tm)
}
