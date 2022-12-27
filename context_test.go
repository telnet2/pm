package pm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithLogDir(t *testing.T) {
	ctx := WithLogDir(context.Background(), "/tmp")
	assert.Equal(t, "/tmp", CtxLogDir(ctx))

	ctx2 := WithLogDir(ctx, "/log")
	assert.Equal(t, "/log", CtxLogDir(ctx2))
}

func TestConfDir(t *testing.T) {
	ctx := WithLogDir(context.Background(), "/tmp")
	ctx2 := WithConfDir(ctx, "/conf")

	assert.Equal(t, "/tmp", CtxLogDir(ctx2))
	assert.Equal(t, "/conf", CtxConfDir(ctx2))
}
