package pm

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/atomic"
)

func TestGoFunc(t *testing.T) {
	run := atomic.NewBool(false)
	GoFunc(func() {
		run.Store(true)
	})
	assert.True(t, run.Load())
}

func TestDuration(t *testing.T) {
	var d Duration
	assert.NoError(t, json.Unmarshal([]byte(`"10s"`), &d))
	assert.Equal(t, d.Duration, time.Second*10)
}
