package executor

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func SleepAndPrint(msg string, t time.Duration) func() error {
	return func() error {
		time.Sleep(t)
		fmt.Println(msg)
		return nil
	}
}

func SleepAndPrintAndError(msg string, t time.Duration) func() error {
	return func() error {
		time.Sleep(t * time.Microsecond)
		fmt.Println(msg)
		return io.EOF
	}
}

func TestExecutor(t *testing.T) {
	e := NewExecutor()
	//  4   2   3
	//    \  \ /
	//      - 1
	// A task 1 waits for a slow task 2 and a fast task 3.
	// A task 4 doesn't wait.
	e.Add("1", &Task{Fn: SleepAndPrint("1", 500), DependsOn: []string{"2", "3", "4"}})
	e.Add("2", &Task{Fn: SleepAndPrint("2", 1000)})
	e.Add("3", &Task{Fn: SleepAndPrint("3", 0)})
	e.Add("4", &Task{Fn: SleepAndPrint("4", 0)})

	// It should run in the order of Any[2,3,4] and [1]
	tasks := e.prepare()
	assert.Len(t, tasks, 3)

	ctx := context.TODO()
	res := e.Execute(ctx)
	assert.False(t, res.HasError())
}

func TestExecutorWithFailure(t *testing.T) {
	e := NewExecutor()
	// 3  4
	// |  |
	// |  2
	//  \ |
	//    1
	// A task 1 waits for a slow task 2 and a fast task 3.
	// A task 4 doesn't wait.
	e.Add("1", &Task{Fn: SleepAndPrint("1", 500), DependsOn: []string{"2", "3"}})
	e.Add("2", &Task{Fn: SleepAndPrintAndError("2", 0), DependsOn: []string{"4"}})
	e.Add("3", &Task{Fn: SleepAndPrint("3", 0)})
	e.Add("4", &Task{Fn: SleepAndPrint("4", 0)})

	tasks := e.prepare()
	assert.Len(t, tasks, 2)

	ctx := context.TODO()
	res := e.Execute(ctx)
	assert.True(t, res.HasError())
	assert.Error(t, res[3].Err) // 1 has ErrSkipExec
}

func TestExecutorWithPanic(t *testing.T) {
	e := NewExecutor()
	// 3 - 4 - 2 (panic) - 1
	// A task 1 waits for a slow task 2 and a fast task 3.
	// A task 4 doesn't wait.
	e.Add("1", &Task{Fn: SleepAndPrint("1", 500), DependsOn: []string{"2", "3"}})
	e.Add("2", &Task{Fn: func() error {
		panic("2")
	}, DependsOn: []string{"4"}})
	e.Add("3", &Task{Fn: SleepAndPrint("3", 0)})
	e.Add("4", &Task{Fn: SleepAndPrint("4", 0), DependsOn: []string{"3"}})

	tasks := e.prepare()
	assert.Len(t, tasks, 1)

	ctx := context.TODO()
	res := e.Execute(ctx)
	assert.True(t, res.HasError())
	assert.Error(t, res[2].Err)
	assert.Error(t, res[3].Err) // 1 has ErrSkipExec
}
