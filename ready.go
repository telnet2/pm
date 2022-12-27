package pm

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/creasty/defaults"
	"github.com/go-resty/resty/v2"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"
	"github.com/telnet2/pm/cmd"
	"github.com/telnet2/pm/debug"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func init() {
	// Suppress MySQL driver error
	_ = mysqlDriver.SetLogger(log.New(io.Discard, "", 0))
}

var (
	// type assertion
	_ ReadyChecker = &DummyChecker{}
	_ ReadyChecker = &LogChecker{}
	_ ReadyChecker = &HttpChecker{}
	_ ReadyChecker = &MySqlChecker{}
	_ ReadyChecker = &MongoDbChecker{}
	_ ReadyChecker = &ShellChecker{}
)

var (
	_logReady = debug.NewDebugLogger("pm:ready")
)

// NewReadyChecker is a factory returning a concrete ReadyChecker
func NewReadyChecker(id string, spec *ReadySpec) ReadyChecker {
	if spec == nil {
		return &DummyChecker{}
	}

	if spec.StdoutLog != "" {
		_logReady.Infof("[%s] create stdout log checker", id)
		return &LogChecker{id: id, spec: spec}
	}

	if spec.Http != nil {
		return &HttpChecker{id: id, spec: spec}
	}

	if spec.MySql != nil {
		return &MySqlChecker{id: id, spec: spec}
	}

	if spec.MongoDb != nil {
		return &MongoDbChecker{id: id, spec: spec}
	}

	if spec.Shell != nil {
		return &ShellChecker{id: id, spec: spec}
	}

	if spec.Tcp != nil {
		return &TcpChecker{id: id, spec: spec}
	}

	return &DummyChecker{}
}

// DummyChecker do nothing
type DummyChecker struct {
}

func (*DummyChecker) Init(context.Context, *Runnable) error {
	return nil
}

func (*DummyChecker) WaitForReady(context.Context, *Runnable) error {
	return nil
}

// ShellChecker is a a shell Script based ready checker.
// A shell command is executed periodically until it returns 0 exit code.
type ShellChecker struct {
	id   string
	spec *ReadySpec
}

// Init is called before the runnable starts.
func (sc *ShellChecker) Init(ctx context.Context, r *Runnable) error {
	return nil
}

// WaitForReady waits until a given runnable is ready.
func (rc *ShellChecker) WaitForReady(ctx context.Context, _ *Runnable) error {
	spec := rc.spec.Shell
	if spec.Interval.Duration == 0 {
		spec.Interval.Duration = time.Second * 3
	}
	if rc.spec.Timeout.Duration == 0 {
		rc.spec.Timeout.Duration = time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, rc.spec.Timeout.Duration)
	tick := time.NewTicker(spec.Interval.Duration)
	defer func() {
		tick.Stop()
		cancel()
	}()

	for {
		exeCmd := cmd.NewCmd(fmt.Sprintf("%s.ready_check", rc.id), spec.Command)
		_logReady.Printf("[%s]: %s", exeCmd.Name, spec.Command)
		select {
		case status := <-exeCmd.Start():
			_logReady.Printf("[%s]: exit %d", exeCmd.Name, status.Exit)
			if status.Error != nil {
				_logReady.Printf("[%s]: error %v", exeCmd.Name, status.Error)
			}
			if status.Exit == 0 {
				return nil
			}
			<-tick.C
		case <-ctx.Done():
			// timeout occurred
			_logReady.Printf("[%s]: timeout", exeCmd.Name)
			_ = exeCmd.Stop()
			return errors.New("timeout error")
		}
	}
}

// Implements ReadyChecker
type LogChecker struct {
	id          string
	spec        *ReadySpec
	logListener chan string
	re          *regexp.Regexp
}

// Init is called before the runnable starts.
func (rc *LogChecker) Init(ctx context.Context, r *Runnable) error {
	re, err := regexp.Compile(rc.spec.StdoutLog)
	if err != nil {
		_logReady.Errorf("[%s] regex compile error: %s", rc.id, rc.spec.StdoutLog)
	} else {
		_logReady.Infof("[%s] regex compile ok: %s", rc.id, rc.spec.StdoutLog)
		rc.re = re
	}

	if r.Cmd == nil || r.Cmd.StdoutPub == nil {
		return errors.New("invalid runnable")
	}
	_logReady.Infof("subscribe stdout to %s", rc.id)
	rc.logListener = r.Cmd.StdoutPub.Subscribe()
	return nil
}

// WaitForReady waits until a given runnable is ready.
func (rc *LogChecker) WaitForReady(ctx context.Context, _ *Runnable) error {
	for {
		select {
		case msg, ok := <-rc.logListener:
			if msg != "" {
				_logReady.Debugf("[%s|stdout] %s", rc.id, msg)
				if rc.re == nil {
					if strings.Contains(msg, rc.spec.StdoutLog) {
						return nil
					}
				} else {
					if rc.re.MatchString(msg) {
						return nil
					}
				}
			}
			if !ok {
				// the listener channel will be closed when the process exits.
				return errors.Errorf("[%s] process exited", rc.id)
			}
		case <-ctx.Done():
			return errors.New("timeout error")
		}
	}
}

type HttpChecker struct {
	id   string
	spec *ReadySpec
}

// Init does nothing.
func (rc *HttpChecker) Init(ctx context.Context, _ *Runnable) error {
	return nil
}

// WaitForReady makes an http call to the url and matches the response with the expectation.
func (rc *HttpChecker) WaitForReady(ctx context.Context, _ *Runnable) error {
	spec := rc.spec.Http
	if err := defaults.Set(spec); err != nil {
		_logReady.Errorf("fail to set defaults: %v", err)
	}

	if spec.Interval == 0 {
		spec.Interval = time.Second * 3
	}
	if rc.spec.Timeout.Duration == 0 {
		rc.spec.Timeout.Duration = time.Minute
	}

	if spec.Expect == nil {
		// expect 200 OK
		spec.Expect = &HttpReadyExpect{Status: 200}
	}

	// compile expect.body regex
	var re *regexp.Regexp
	if spec.Expect.BodyRegex != "" {
		var err error
		re, err = regexp.Compile(spec.Expect.BodyRegex)
		if err != nil {
			_logReady.Errorf("[%s] fail to compile regex: %s", spec.Expect.BodyRegex)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, rc.spec.Timeout.Duration)
	tick := time.NewTicker(spec.Interval)
	defer func() {
		cancel()
		tick.Stop()
	}()

	cli := resty.New()
	// No redirection but use the last response.
	cli.SetRedirectPolicy(resty.RedirectPolicyFunc(
		func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}))

	var err error
	for {
		var res *resty.Response

		expect := spec.Expect
		// Set the context with timeout
		res, err = cli.R().SetContext(ctx).Get(spec.URL)

		// if there was no error in the connection, validate the response with the expectation.
		if err == nil {
			ok := true
			if expect.Status != 0 {
				if res.StatusCode() != expect.Status {
					ok = false
					err = errors.Errorf("[%s] status code mismatch %d != %d", rc.id, expect.Status, res.StatusCode())
				}
			}
			if ok && re != nil {
				ok = ok && re.Match(res.Body())
			}
			if ok {
				// match header values
				for k, v := range spec.Expect.Headers {
					var matched bool
					matched, err = regexp.MatchString(v, res.Header().Get(k))
					if err == nil {
						ok = ok && matched
						if !matched {
							err = errors.Errorf("[%s] header %s mismatch: %s != %s", rc.id, k, v, res.Header().Get(k))
						}
					} else {
						_logReady.Errorf("[%s] http.expect.headers.%s regex error: v", k, v)
					}
				}
			}
			if ok {
				return nil
			}
		}

		select {
		case <-tick.C:
			// repeat
		case <-ctx.Done():
			return err
		}
	}
}

type TcpChecker struct {
	id   string
	spec *ReadySpec
	n    int
}

func (c *TcpChecker) RetryCount() int {
	return c.n
}

// Init is called before the runnable starts.
func (c *TcpChecker) Init(ctx context.Context, r *Runnable) error {
	return nil
}

// WaitForReady waits until the port is open.
func (c *TcpChecker) WaitForReady(ctx context.Context, _ *Runnable) error {
	spec := c.spec.Tcp
	if spec.Interval.Duration == 0 {
		spec.Interval.Duration = time.Second * 3
	}
	if c.spec.Timeout.Duration == 0 {
		c.spec.Timeout.Duration = time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, c.spec.Timeout.Duration)
	tick := time.NewTicker(spec.Interval.Duration)
	defer func() {
		cancel()
		tick.Stop()
	}()

	for c.n = 0; ; c.n++ {
		cli, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", spec.Host, spec.Port), time.Second)
		if err == nil {
			_ = cli.Close()
			return nil
		}

		select {
		case <-tick.C:
			// repeat
		case <-ctx.Done():
			return err
		}
	}
}

type MongoDbChecker struct {
	id   string
	spec *ReadySpec
}

// Init does nothing.
func (rc *MongoDbChecker) Init(ctx context.Context, r *Runnable) error {
	return nil
}

// WaitForReady makes an http call to the url and matches the response with the expectation.
func (rc *MongoDbChecker) WaitForReady(ctx context.Context, r *Runnable) error {
	spec := rc.spec.MongoDb
	if err := defaults.Set(spec); err != nil {
		_logReady.Errorf("fail to set defaults: %v", err)
	}

	if spec.Interval == 0 {
		spec.Interval = time.Second * 3
	}
	if rc.spec.Timeout.Duration == 0 {
		rc.spec.Timeout.Duration = time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, rc.spec.Timeout.Duration)
	tick := time.NewTicker(spec.Interval)
	defer func() {
		cancel()
		tick.Stop()
	}()

	cliOpts := options.Client().ApplyURI(spec.URI)
	var err error
	for {
		var mongoCli *mongo.Client
		// mongodb://[username:password@]host1[:port1][,...hostN[:portN]][/[defaultauthdb][?options]]
		mongoCli, err = mongo.Connect(ctx, cliOpts)
		if err == nil {
			if err = mongoCli.Ping(ctx, nil); err == nil {
				_logReady.Printf("[mongo] ready: %s", spec.URI)
				return nil
			} else {
				_logReady.Printf("[mongo] ping failed: %s error: %v", spec.URI, err)
			}
		} else {
			_logReady.Printf("[mongo] connect failed: %s error: %v", spec.URI, err)
		}

		var cmdDone <-chan struct{}
		if r != nil && r.Cmd != nil {
			cmdDone = r.Cmd.Done()
		}

		select {
		case <-tick.C:
			// repeat
		case <-cmdDone:
			// if it is already shutdown
			cmdStatus := r.Cmd.Status()
			return fmt.Errorf("[%s] is already stopped: %d => %v", rc.id, cmdStatus.Exit, cmdStatus.Error)
		case <-ctx.Done():
			return err
		}
	}
}

type MySqlChecker struct {
	id   string
	spec *ReadySpec
}

// Init does nothing.
func (rc *MySqlChecker) Init(ctx context.Context, r *Runnable) error {
	return nil
}

// WaitForReady makes an http call to the url and matches the response with the expectation.
func (rc *MySqlChecker) WaitForReady(ctx context.Context, r *Runnable) error {
	spec := rc.spec.MySql
	if err := defaults.Set(spec); err != nil {
		_logReady.Errorf("fail to set defaults: %v", err)
	}

	if spec.Interval == 0 {
		spec.Interval = time.Second * 3
	}
	if rc.spec.Timeout.Duration == 0 {
		rc.spec.Timeout.Duration = time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, rc.spec.Timeout.Duration)
	tick := time.NewTicker(spec.Interval)
	defer func() {
		cancel()
		tick.Stop()
	}()

	var err error
	for {
		var db *sql.DB
		dsn := fmt.Sprintf("%s:%s@%s(%s)/%s?timeout=1s", spec.User, spec.Password, spec.Network, spec.Addr, spec.Database)
		_logReady.Infof("[%s] checking mysql: %s", rc.id, dsn)

		db, err = sql.Open("mysql", dsn)
		if err == nil {
			_, err = db.QueryContext(ctx, fmt.Sprintf("select 1 from %s", spec.Table))
			if err == nil {
				_logReady.Debugf("[%s] is ready", rc.id)
				return nil
			} else {
				_logReady.Debugf("[%s] ready check error: %v", rc.id, err)
			}
		} else {
			_logReady.Debugf("[%s] ready check error: %v", rc.id, err)
		}

		var cmdDone <-chan struct{}
		if r != nil && r.Cmd != nil {
			cmdDone = r.Cmd.Done()
		}

		select {
		case <-tick.C:
			// repeat
		case <-cmdDone:
			// if it is already shutdown
			cmdStatus := r.Cmd.Status()
			return fmt.Errorf("[%s] is already stopped: %d => %v", rc.id, cmdStatus.Exit, cmdStatus.Error)
		case <-ctx.Done():
			return err
		}
	}
}
