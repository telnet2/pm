package debug

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDetermineEnabled(t *testing.T) {
	tag := "pm:executor"
	assert.False(t, determineEnabled(tag, ""))
	assert.False(t, determineEnabled(tag, "pm:loader"))
	assert.True(t, determineEnabled(tag, "pm:executor"))
	assert.True(t, determineEnabled(tag, "pm:*"))
	assert.True(t, determineEnabled(tag, "pm:(loader|executor)"))
	assert.True(t, determineEnabled(tag, "pm:loader,pm:executor"))
}

func TestDebugLoggerLogLevel(t *testing.T) {
	oldDebug, ok := os.LookupEnv("DEBUG")
	os.Setenv("DEBUG", "xxx")
	logger := NewDebugLogger("xxx")
	logger.Infof("this is a info log.")
	logger.Warnf("this is a warn log.")
	logger.Errorf("this is an error log.")
	if ok {
		os.Setenv("DEBUG", oldDebug)
	}
}

func TestDebugLoggerRace(t *testing.T) {
	wg := sync.WaitGroup{}
	l := NewDebugLogger("yyy")
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			for k := 0; k < 1000; k++ {
				l.Infof("hello %d", i)
				time.Sleep(time.Microsecond * 10)
			}
			wg.Done()
		}(i)
	}
	wg.Wait()
}
