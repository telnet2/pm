package ps

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func XTestFindProcess(t *testing.T) {
	proc, err := FindProcess(os.Getppid())
	assert.NoError(t, err)
	if proc != nil {
		assert.NotEmpty(t, proc.Children)
		fmt.Println(proc.Tree())
	}
}
