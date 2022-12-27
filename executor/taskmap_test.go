package executor

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTriggerSet(t *testing.T) {
	tset := NewTriggerSet()
	tset.Add("1")
	tset.Add("2")

	assert.False(t, tset.Remove("1"))
	assert.True(t, tset.Remove("2"))
}

func TestTask(t *testing.T) {
	task := Task{}
	assert.False(t, task.IsDone())
	task.MarkDone()
	assert.True(t, task.IsDone())
}

func TestTaskResult(t *testing.T) {
	taskResult := &(TaskResult{&Task{}, &Task{}})
	assert.False(t, taskResult.HasError())

	(*taskResult)[0].Err = errors.New("task error")
	assert.True(t, taskResult.HasError())
	assert.NotEmpty(t, taskResult.Error())
}

func TestTaskMap(t *testing.T) {
	taskMap := NewTaskMap()

	t1 := &Task{Id: "task1"}
	t2 := &Task{Id: "task2"}

	taskMap.AddTask(t1)
	taskMap.AddTask(t2)

	assert.Equal(t, taskMap.Count(), 2)
	assert.Equal(t, t1, taskMap.Get("task1"))
	assert.Equal(t, t2, taskMap.Get("task2"))

	assert.ElementsMatch(t, []string{"task1", "task2"}, taskMap.GetTaskIds())

	taskIds := []string{}
	taskMap.Range(func(s string, tt *Task) bool {
		assert.Equal(t, s, tt.Id)
		taskIds = append(taskIds, tt.Id)
		return true
	})
	assert.ElementsMatch(t, []string{"task1", "task2"}, taskIds)

	taskMap.RemoveTask(t1)
	assert.Equal(t, taskMap.Count(), 1)
	assert.ElementsMatch(t, []string{"task2"}, taskMap.GetTaskIds())

}
