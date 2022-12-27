package executor

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/telnet2/pm/debug"
)

var (
	ErrExecSkipped = errors.New("not executed")
	ErrPanic       = errors.New("panic error")

	_logExecutor = debug.NewDebugLogger("pm:exec")
)

type Executor struct {
	Tasks     *TaskMap
	runCh     chan *Task
	collectCh chan *Task
	executing int32
}

// NewExecutor creates a new Executor instance with a new TaskMap.
func NewExecutor() *Executor {
	return &Executor{
		Tasks: NewTaskMap(),
	}
}

// Add adds a task to the executor.
func (e *Executor) Add(id string, t *Task) {
	if e.Tasks == nil {
		e.Tasks = NewTaskMap()
	}
	t.Id = id
	e.Tasks.AddTask(t)
}

// collect collects the executation status of tasks.
func (e *Executor) collect() TaskResult {
	finalCount := e.Tasks.Count()
	result := TaskResult{}

	for task := range e.collectCh {
		result = append(result, task)
		if task.Err != nil {
			// cancel running tasks and stop executing new tasks
			atomic.StoreInt32(&e.executing, 0)
		}

		if len(result) == finalCount {
			// if every task has been collected,
			// safely close channel
			close(e.collectCh)
			close(e.runCh)
		}
	}
	return result
}

// exec executes tasks one by one from e.runCh channel.
// It spawns a new go-routine to execute a task Fn and waits for the task to finish.
func (e *Executor) exec() {
	// exec go-routine reads from
	for task := range e.runCh {
		_logExecutor.Infof("[%s] starting to execute", task.Id)

		t := task
		// start a new go-routine to launch a new task.
		// this go-routine will call Runnable.WaitForReady()
		go func() {
			// after t.Fn is called, prepare to call its children tasks.
			defer func() {
				err := recover() // prevent panic from t.Fn
				if err != nil {
					_logExecutor.Errorf("panic: %v", err)
					t.Err = ErrPanic
				}

				t.MarkDone()
				e.collectCh <- t

				// search tasks to run next
				e.Tasks.Range(func(_ string, n *Task) bool {
					if !n.IsDone() && !n.Running {
						n.Skip = n.Skip || t.Err != nil
						if n.priors.Remove(t.Id) {
							n.Running = true // prevent running multiple times
							e.runCh <- n     // shouldn't block
						}
					}
					return true
				})
			}()

			if t.Skip || atomic.LoadInt32(&e.executing) == 0 {
				t.Err = ErrExecSkipped // Skip the execution
			} else {
				t.Err = t.Fn()
			}
		}()
	}
}

// Execute runs every task and wait until every task is executed.
func (e *Executor) Execute(ctx context.Context) TaskResult {
	defer func() {
		atomic.StoreInt32(&e.executing, 0)
	}()
	if atomic.LoadInt32(&e.executing) == 1 {
		// Do not run again in the middle of the execution.
		return nil
	}
	atomic.StoreInt32(&e.executing, 1)

	e.runCh = make(chan *Task, e.Tasks.Count())
	e.collectCh = make(chan *Task, e.Tasks.Count())

	roots := e.prepare()
	if len(roots) == 0 {
		// there's no root tasks without a dependency.
		// then return empty.
		_logExecutor.Errorf("empty root task")
		return TaskResult{}
	}

	var rootNames []string
	for _, r := range roots {
		rootNames = append(rootNames, r.Id)
	}
	_logExecutor.Infof("identified root tasks: %v", rootNames)

	for _, t := range roots {
		t.Running = true
		e.runCh <- t
	}
	go e.exec()
	return e.collect()
}

// prepare identifies the root tasks.
func (e *Executor) prepare() []*Task {
	roots := []*Task{}
	e.Tasks.Range(func(_ string, t *Task) bool {
		t.Err = nil
		t.Running = false
		atomic.StoreInt32(&t.done, 0)
		if len(t.DependsOn) == 0 {
			roots = append(roots, t)
		} else {
			t.priors = NewTriggerSet()
			for _, d := range t.DependsOn {
				t.priors.Add(d)
			}
		}
		return true
	})
	return roots
}
