package pm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/telnet2/pm/cmd"
	"github.com/telnet2/pm/debug"
	"github.com/telnet2/pm/executor"
	"github.com/telnet2/pm/shutdown"

	"github.com/pkg/errors"
)

var (
	_logProcMan = debug.NewDebugLogger("pm:procman")
)

// ProcMan implements the process manager.
type ProcMan struct {
	RunGraph map[string]*Runnable // RunGraph is the map from the id of a runnable to its contents

	eventPub ProcEventPublisher // eventPub is the process state publisher

	isStarted            bool  // indicate if it is started
	lastError            error // records the last error occurred during Start
	isShutdownInProgress int32
}

func NewProcMan() *ProcMan {
	return &ProcMan{
		RunGraph: map[string]*Runnable{},
		eventPub: *NewProcEventPublisher(100*time.Millisecond, 10),
	}
}

// addRunnable initializes a given runnable with Cmd and adds to the RunGraph.
func (pm *ProcMan) addRunnable(runnable *Runnable) {
	if runnable == nil || runnable.Id == "" || runnable.GetCommand() == "" {
		// do nothing if runnable is empty.
		_logProcMan.Printf("invalid runnable: %v", runnable)
		return
	}

	// create a mutex if needed
	if runnable.l == nil {
		runnable.l = &sync.Mutex{}
	}

	if runnable.Cmd != nil {
		// if there was a Cmd, then stop it.
		// it should be safe to call Stop multiple times.
		_ = runnable.Cmd.Stop()
	}

	runnable.Cmd = cmd.NewCmdOptions(
		cmd.Options{
			Streaming: true,
			BeforeExec: []func(_ *exec.Cmd){
				func(_ *exec.Cmd) {
					pm.eventPub.Publish(&ProcEvent{Id: runnable.Id, Event: "start"})
				},
			},
			AfterDone: []func(_ *cmd.Cmd){
				func(proc *cmd.Cmd) {
					// when done, publish "done"
					result := proc.Status()
					pm.eventPub.Publish(&ProcEvent{Id: runnable.Id, Event: "done", ExitCode: result.Exit})
					if pm.IsAllDone() {
						pm.eventPub.Publish(&ProcEvent{Event: "alldone"})
					}
				},
			},
		},
		runnable.Id,
		runnable.Command,
	)
	runnable.Cmd.Dir = runnable.WorkDir
	runnable.Cmd.LogDir = runnable.LogDir
	runnable.Cmd.Env = runnable.GetRuntimeEnvs()
	runnable.Ready = NewReadyChecker(runnable.Id, runnable.ReadyCondition)

	pm.RunGraph[runnable.Id] = runnable
}

// Add adds a runnable to the ProcMan.
// If the id isn't unique, returns an error.
func (pm *ProcMan) Add(run *Runnable) error {
	if _, exists := pm.RunGraph[run.Id]; exists {
		return fmt.Errorf("%s already exists", run.Id)
	}
	pm.addRunnable(run)
	return nil
}

// AddConfig adds services from a given config file or a struct.
func (pm *ProcMan) AddConfig(configFile *ConfigFile) error {
	for id, service := range configFile.Services {
		service.Id = id
		if err := pm.Add(service); err != nil {
			_logProcMan.Printf("adding: %s error: %v", service.Id, err)
			return err
		}
	}
	return nil
}

// WaitDone waits until all processes are finished.
func (pm *ProcMan) WaitDone() {
	// should not allow restart??
	if !pm.isStarted || pm.IsAllDone() {
		// if it was already done; return
		return
	}

	for id, r := range pm.RunGraph {
		_logProcMan.Infof("[%s] waiting to be done", id)
		if !r.Cmd.IsDone() {
			<-r.Cmd.Done()
		}
		_logProcMan.Infof("[%s] is done", id)
	}
}

// Shutdown closes the ProcMan and releases all the resources.
func (pm *ProcMan) Shutdown(err error) {
	if !atomic.CompareAndSwapInt32(&pm.isShutdownInProgress, 0, 1) {
		return
	}
	if err != nil {
		_logProcMan.Infof("start to shutdown due to %v", err)
	} else {
		_logProcMan.Infof("start to shutdown")
	}
	wg := sync.WaitGroup{}
	for _, r := range pm.RunGraph {
		wg.Add(1)
		go func(r *Runnable) {
			err := pm.Stop(r.Id)
			if err != nil {
				_logProcMan.Warnf("[%s] fail to stop: %v", r.Id, err)
			} else {
				_logProcMan.Infof("[%s] stopped", r.Id)
			}
			wg.Done()
		}(r)
	}
	wg.Wait()
	atomic.StoreInt32(&pm.isShutdownInProgress, 0)
}

// Stop stops the process
func (pm *ProcMan) Stop(id string) error {
	run, ok := pm.RunGraph[id]
	if !ok {
		return errors.Errorf("can't find %s", id)
	}
	_logProcMan.Infof("[%s] stopping pid=%d", id, run.Cmd.Status().PID)
	if err := run.Cmd.Stop(); err != nil {
		return err
	}
	// Wait until done for 10 secs
	timer := time.NewTimer(time.Second * 10)
	defer timer.Stop()
	select {
	case <-run.Cmd.Done():
		return nil
	case <-timer.C:
		return errors.Errorf("[%s] fail to stop; timeout", id)
	}
}

// Restart restarts the service.
// It is different from Start() in that it only restarts the runnable with a given id.
// Also, it starts only a given runnable not its dependents.
func (pm *ProcMan) Restart(ctx context.Context, id string) error {
	run, ok := pm.RunGraph[id]
	if !ok {
		return errors.Errorf("can't find %s", id)
	}

	err := run.Cmd.Stop()
	if err != nil {
		return err
	}

	run.CloneCmd()

	// send start event
	run.Cmd.Start()
	return nil
}

// IsAllDone returns true if every runnable is done
func (pm *ProcMan) IsAllDone() bool {
	// TODO: how to solve concurrency issue
	for _, r := range pm.RunGraph {
		if !r.IsDone() {
			return false
		}
	}

	return true
}

// enqueue adds a given runnable to a given executor.
// It registers OnStop hook for Cmd, publishes `start` event, and adds the runnable in the Executor.
func (pm *ProcMan) enqueue(ctx context.Context, exec *executor.Executor, runnable *Runnable) {
	exec.Add(runnable.Id, &executor.Task{
		Fn: func() error {
			if err := runnable.Ready.Init(ctx, runnable); err != nil {
				return err
			}
			_logProcMan.Infof("[%s] waiting for ready", runnable.Id)
			runnable.Cmd.Start()

			// if ready condition is given, check readiness of this task.
			var err error
			if runnable.ReadyCondition != nil {
				err = runnable.Ready.WaitForReady(ctx, runnable)
				if err != nil {
					_logProcMan.Errorf("[%s] fail to wait %v", runnable.Id, err)
				} else {
					_logProcMan.Infof("[%s] ready", runnable.Id)
					pm.eventPub.Publish(&ProcEvent{Id: runnable.Id, Source: runnable, Event: "ready"})
				}
			}
			return err
		},
		DependsOn: runnable.DependsOn,
	})
}

// Start executes all the runnables in the order of its dependencies
// and waits for its execution.
func (pm *ProcMan) Start(ctx context.Context) error {
	exec := executor.NewExecutor()

	// Register a shutdown hook
	shutdown.AddWithKeyWithParam("procman", func(sig os.Signal) {
		// if it is a interrupt or a kill signal, shutdown the procman.
		_logProcMan.Infof("signal received: %d", sig)
		if sig == os.Interrupt || sig == os.Kill {
			pm.Shutdown(nil)
		}
	})

	// Reset status indicators.
	pm.isStarted = true
	pm.lastError = nil

	// Add runnables into the executor.
	// The executor find root tasks to start.
	for _, _r := range pm.RunGraph {
		pm.enqueue(ctx, exec, _r)
	}

	res := exec.Execute(ctx)
	if res.HasError() {
		pm.lastError = &res
		_logProcMan.Errorf(res.Error())
		pm.Shutdown(pm.lastError)
		return fmt.Errorf("fail to execute: %v", res.Error())
	}

	return nil
}

// SubscribeLog subscribes to the log publisher with an `id`.
// The service `id` must be already added to this ProcMan.
func (pm *ProcMan) SubscribeLog(id string, stdout bool) chan string {
	rn := pm.RunGraph[id]
	if rn != nil {
		if rn.Cmd != nil {
			if rn.Cmd.StdoutPub != nil {
				_logProcMan.Infof("subscribe to the stdout of[%s]", rn.Id)
				return rn.Cmd.StdoutPub.Subscribe()
			}
		}
	}
	return nil
}

// SubscribeEvent subscribes to the events of the id.
// If the id is '*', it listens all the events.
func (pm *ProcMan) SubscribeEvent(id string, events map[string]struct{}) chan *ProcEvent {
	return pm.eventPub.SubscribeTopic(func(e *ProcEvent) bool {
		if events == nil {
			return id == e.Id
		}
		_, eventMatched := events[e.Event]
		return (id == "*" || id == e.Id) && eventMatched
	})
}

// UnsubscribeEvent
func (pm *ProcMan) UnsubscribeEvent(sub chan *ProcEvent) {
	pm.eventPub.Evict(sub)
}
