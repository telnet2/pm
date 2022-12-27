package executor

import (
	"strings"
	"sync"
	"sync/atomic"
)

// TriggerSet is a set that returns true if the set is empty after removing an item.
// Its functions are all thread-safe.
type TriggerSet struct {
	m map[string]struct{}
	l *sync.Mutex
}

func NewTriggerSet() *TriggerSet {
	return &TriggerSet{
		m: make(map[string]struct{}),
		l: &sync.Mutex{},
	}
}

func (t *TriggerSet) Add(id string) {
	defer t.l.Unlock()
	t.l.Lock()
	t.m[id] = struct{}{}
}

func (t *TriggerSet) Remove(id string) bool {
	defer t.l.Unlock()
	t.l.Lock()
	delete(t.m, id)
	return len(t.m) == 0
}

// Task is an object representing a named task and its next tasks.
type Task struct {
	Id        string
	Fn        func() error
	DependsOn []string
	Err       error // execution error
	Running   bool  // this task is running
	Skip      bool  // this task is skipped due to an error from prior tasks
	done      int32 // this task is done or skipped.

	priors *TriggerSet
}

func (t *Task) MarkDone() {
	atomic.StoreInt32(&t.done, 1)
}

func (t *Task) IsDone() bool {
	return atomic.LoadInt32(&t.done) > 0
}

type TaskResult []*Task

func (t *TaskResult) HasError() bool {
	if len(*t) == 0 {
		return true
	}
	for _, r := range *t {
		if r.Err != nil {
			return true
		}
	}
	return false
}

// Error returns a combined error message.
func (t *TaskResult) Error() string {
	if len(*t) == 0 {
		return "empty task result"
	}
	errMsg := strings.Builder{}
	for _, r := range *t {
		if r.Err != nil {
			errMsg.WriteString(r.Id)
			errMsg.WriteString("=>")
			errMsg.WriteString(r.Err.Error())
			errMsg.WriteString("\t")
		}
	}
	return errMsg.String()
}

// TaskMap is a synchronized map to store tasks using its Id as a key.
type TaskMap struct {
	m map[string]*Task
	l *sync.Mutex
}

func NewTaskMap() *TaskMap {
	return &TaskMap{
		m: make(map[string]*Task),
		l: &sync.Mutex{},
	}
}

func (t *TaskMap) Count() int {
	return len(t.m)
}

func (t *TaskMap) AddTask(k *Task) {
	defer t.l.Unlock()
	t.l.Lock()
	t.m[k.Id] = k
}

func (t *TaskMap) RemoveTask(k *Task) {
	defer t.l.Unlock()
	t.l.Lock()
	delete(t.m, k.Id)
}

func (t *TaskMap) GetTaskIds() []string {
	defer t.l.Unlock()
	t.l.Lock()
	var ids []string
	for id := range t.m {
		ids = append(ids, id)
	}
	return ids
}

func (t *TaskMap) Get(id string) *Task {
	defer t.l.Unlock()
	t.l.Lock()
	return t.m[id]
}

func (t *TaskMap) Range(iterFn func(string, *Task) bool) {
	defer t.l.Unlock()
	t.l.Lock()

	for id, tk := range t.m {
		if !iterFn(id, tk) {
			break
		}
	}
}
