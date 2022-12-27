package pm

import "context"

type contextKey int

const (
	logDir = iota
	confDir

	logDirKey  = contextKey(logDir)
	confDirKey = contextKey(confDir)
)

func WithLogDir(ctx context.Context, logDir string) context.Context {
	return context.WithValue(ctx, logDirKey, logDir)
}

func CtxLogDir(ctx context.Context) string {
	return ctx.Value(logDirKey).(string)
}

func WithConfDir(ctx context.Context, confDir string) context.Context {
	return context.WithValue(ctx, confDirKey, confDir)
}

func CtxConfDir(ctx context.Context) string {
	confDir, ok := ctx.Value(confDirKey).(string)
	if ok {
		return confDir
	}
	return ""
}
