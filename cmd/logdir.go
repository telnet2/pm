package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var (
	reName = regexp.MustCompile(`[/$|'"]`)
)

// CreateLogFile helps to create a log file for a command.
func CreateLogFile(logdir, name string, stdout bool) (*os.File, error) {
	pattern := "%s.stderr.log"
	if stdout {
		pattern = "%s.stdout.log"
	}
	safename := reName.ReplaceAllString(name, ".")
	logFile := filepath.Join(logdir, fmt.Sprintf(pattern, safename))
	// make a backup file
	_, err := os.Stat(logFile)
	if err == nil {
		_ = os.Rename(logFile, logFile+".bak")
	}
	return os.Create(logFile)
}
