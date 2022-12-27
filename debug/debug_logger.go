package debug

import (
	"bufio"
	"bytes"
	"fmt"
	"hash/crc64"
	"log"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gookit/color"
	"github.com/kr/pretty"
)

type LogFn func(format string, v ...interface{})

// DebugLogger is a container struct for the DebugLogger logger used to manage internal state.
// LogLevel is determined by log specifier coming after # character of DEBUG env var.
// e.g.) DEBUG=pm:cmd#i,d  means prints pm:cmd logs with info & debug logs.
type DebugLogger struct {
	tag     string
	allowed string
	enabled bool
	logger  *log.Logger
	color   bool
	last    int64

	Debugf LogFn
	Warnf  LogFn
	Infof  LogFn
	Errorf LogFn
}

var (
	table  = crc64.MakeTable(crc64.ISO)
	gs     = color.C256(uint8(240))
	colors = []int{20, 21, 26, 27, 32, 33, 38, 39, 40, 41, 42, 43, 44, 45, 56, 57, 62, 63, 68, 69, 74,
		75, 76, 77, 78, 79, 80, 81, 92, 93, 98, 99, 112, 113, 128, 129, 134, 135, 148, 149,
		160, 161, 162, 163, 164, 165, 166, 167, 168, 169, 170, 171, 172, 173, 178, 179,
		184, 185, 196, 197, 198, 199, 200, 201, 202, 203, 204, 205, 206, 207, 208, 209, 214, 215, 220, 221,
	}

	debugPrefix string
	infoPrefix  string
	warnPrefix  string
	errorPrefix string
)

// NewDebugLogger Returns a DebugLogger logging instance.
// It will determine if the logger should bypass logging actions or be activated.
// If NO_COLOR env var is set, color output is disabled.
func NewDebugLogger(tag string) *DebugLogger {
	allowed, level := getDebugFlagFromEnv()

	logger := DebugLogger{tag: tag, allowed: allowed}

	logger.determineLogLevel(level)

	if logger.allowed != "" {
		logger.enabled = determineEnabled(tag, allowed)
		logger.color = os.Getenv("NOCOLOR") == ""
	} else {
		logger.enabled = false
		logger.color = false
	}

	// determineTagLength(&logger)

	logger.refreshLogger()

	return &logger
}

func (logger *DebugLogger) refreshLogger() {
	// defer lock.Unlock()
	// lock.Lock()

	var prefix string
	if logger.enabled {
		tag := logger.tag
		if logger.color {
			s := ChooseColor(tag)
			prefix = s.Sprintf("%s ", tag)

			debugPrefix = color.Debug.Sprint("DEBUG")
			infoPrefix = color.Info.Sprint("INFO ")
			warnPrefix = color.Warn.Sprint("WARN ")
			errorPrefix = color.Error.Sprint("ERROR")
		} else {
			prefix = fmt.Sprintf("%s ", tag)
		}

		logger.logger = log.New(os.Stderr, prefix, log.Lmsgprefix)
		logger.last = time.Now().UnixNano()
	}
}

// Printf is equivalent to fmt.Printf(f, pretty.Formatter(x), pretty.Formatter(y)).
func (k *DebugLogger) Printf(format string, v ...interface{}) {
	if k.enabled {
		elapsed := k.determineElapsed()

		var buf bytes.Buffer
		_, _ = pretty.Fprintf(&buf, format, v...)

		showDelta := true
		k.printBuffer(buf, elapsed, &showDelta)
	}
}

// Println is equivalent to fmt.Println(pretty.Formatter(x), pretty.Formatter(y)),
// but each operand is formatted with "%# v".
func (k *DebugLogger) Println(v ...interface{}) {
	if k.enabled {
		showDelta := true
		for _, x := range v {
			elapsed := k.determineElapsed()

			var buf bytes.Buffer
			_, _ = pretty.Fprintf(&buf, "%# v", x)

			k.printBuffer(buf, elapsed, &showDelta)
		}
	}
}

// Infof print DEBG prefix
func (k *DebugLogger) debugf(format string, v ...interface{}) {
	if k.enabled {
		elapsed := k.determineElapsed()

		var buf bytes.Buffer
		buf.WriteString(debugPrefix)
		buf.WriteString(" ")
		_, _ = pretty.Fprintf(&buf, format, v...)

		showDelta := true
		k.printBuffer(buf, elapsed, &showDelta)
	}
}

// Infof print INFO prefix
func (k *DebugLogger) infof(format string, v ...interface{}) {
	if k.enabled {
		elapsed := k.determineElapsed()

		var buf bytes.Buffer
		buf.WriteString(infoPrefix)
		buf.WriteString(" ")
		_, _ = pretty.Fprintf(&buf, format, v...)

		showDelta := true
		k.printBuffer(buf, elapsed, &showDelta)
	}
}

// Warnf print WARN prefix
func (k *DebugLogger) warnf(format string, v ...interface{}) {
	if k.enabled {
		elapsed := k.determineElapsed()

		var buf bytes.Buffer
		buf.WriteString(warnPrefix)
		buf.WriteString(" ")
		_, _ = pretty.Fprintf(&buf, format, v...)

		showDelta := true
		k.printBuffer(buf, elapsed, &showDelta)
	}
}

// Warnf print WARN prefix
func (k *DebugLogger) errorf(format string, v ...interface{}) {
	if k.enabled {
		elapsed := k.determineElapsed()

		var buf bytes.Buffer
		buf.WriteString(errorPrefix)
		buf.WriteString(" ")
		_, _ = pretty.Fprintf(&buf, format, v...)

		showDelta := true
		k.printBuffer(buf, elapsed, &showDelta)
	}
}

// Log is an alias to Println
func (k *DebugLogger) Log(v ...interface{}) {
	k.Println(v...)
}

// determineLogLevel determines which log level will be printed by DEBUG_LEVEL env var.
// DEBUG_LEVEL is specified with comma separated list of debug/d, info/i, and warn/n flags.
// Error logs are printed always.
func (k *DebugLogger) determineLogLevel(level string) {
	k.Debugf = dummyLogFn
	k.Infof = dummyLogFn
	k.Warnf = dummyLogFn
	k.Errorf = k.errorf

	debugLevel := level
	if level == "" {
		debugLevel = strings.ToLower(os.Getenv("DEBUG_LEVEL"))
	}

	if debugLevel == "" {
		debugLevel = "i"
	}

	for _, l := range strings.Split(debugLevel, ",") {
		switch l {
		case "debug", "d":
			k.Debugf = k.debugf
			k.Infof = k.infof
			k.Warnf = k.warnf
		case "info", "i":
			k.Infof = k.infof
			k.Warnf = k.warnf
		case "warn", "w":
			k.Warnf = k.warnf
		}
	}
}

// Extend returns a new DebugLogger logger instance that has appended the provided tag to the original logger.
//
// New logger instance will have original `tag` value delimited with a `:` and appended with the new extended `tag` input.
//
// Example:
//     k := New("test:original)
//     k.Log("test")
//     ke := k.Extend("plugin")
//     ke.Log("test extended")
//
// Output:
//     test:original test
//     test:original:plugin test extended
func (k *DebugLogger) Extend(tag string) *DebugLogger {
	exTag := fmt.Sprintf("%s:%s", k.tag, tag)
	return NewDebugLogger(exTag)
}

// ChooseColor will return the same color based on input string.
func ChooseColor(tag string) *color.Color256 {
	// Generate an 8 byte checksum to pass into Rand.seed
	seed := crc64.Checksum([]byte(tag), table)
	rand.Seed(int64(seed))
	v := rand.Intn(len(colors) - 1)
	s := color.C256(uint8(colors[v]))
	return &s
}

// printBuffer will append the elapsed time delta to the first line of the provided buffer
// if the showDelta parameter is true. Otherwise, this method prints the buffer lines to STDERR
func (k *DebugLogger) printBuffer(buf bytes.Buffer, elapsed time.Duration, showDelta *bool) {
	s := bufio.NewScanner(&buf)
	for s.Scan() {
		if *showDelta {
			var ft string
			if k.color {
				ft = gs.Sprintf("+%s", elapsed.Truncate(time.Millisecond))
			} else {
				ft = fmt.Sprintf("+%s", elapsed.Truncate(time.Millisecond))
			}
			k.logger.Printf("%s %s\n", s.Text(), ft)
			*showDelta = false
		} else {
			k.logger.Print(s.Text())
		}
	}
}

// getDebugFlagFromEnv considers both the value of DEBUG env values
// to determine the resulting logging flags to pass to the loggers.
func getDebugFlagFromEnv() (string, string) {
	return os.Getenv("DEBUG"), os.Getenv("DEBUG_LEVEL")
}

// determineElapsed will determine the time delta from between the last log event for this
func (k *DebugLogger) determineElapsed() time.Duration {
	now := time.Now().UnixNano()
	elapsed := now - atomic.LoadInt64(&k.last)
	atomic.StoreInt64(&k.last, now)

	return time.Duration(elapsed)
}

// determineEnabled will check the value of DEBUG environment variables to generate regex to test against the tag
//
// If no * in string, then assume exact match
// Else
// It will split by , and perform
// It will, replace * with .*
func determineEnabled(tag string, allowed string) bool {
	var a bool
	for _, l := range strings.Split(allowed, ",") {
		reg := strings.ReplaceAll(l, "*", ".*")
		if !strings.HasPrefix(reg, "^") {
			reg = fmt.Sprintf("^%s", reg)
		}

		if !strings.HasSuffix(reg, "$") {
			reg = fmt.Sprintf("%s$", reg)
		}

		a, _ = regexp.Match(reg, []byte(tag))
		if a {
			return true
		}
	}
	return false
}

// dummyLogFn does nothing and is used for hiding logs out of a given level.
func dummyLogFn(format string, v ...interface{}) {}
