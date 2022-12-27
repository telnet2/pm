package pm

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/telnet2/pm/cmd"
)

var once = sync.Once{}

func initRunnable(runnable *Runnable) {
	if runnable.Cmd == nil {
		runnable.Cmd = cmd.NewCmdOptions(
			cmd.Options{Streaming: true},
			runnable.Id,
			runnable.Command,
		)
		runnable.Cmd.Dir = runnable.WorkDir
		runnable.Cmd.Env = runnable.Env
	}
}

func TestLogCheckerTimeout(t *testing.T) {
	rc := NewReadyChecker("test", &ReadySpec{StdoutLog: "done"})
	lc := rc.(*LogChecker)
	lc.logListener = make(chan string, 1)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// must error after 1 sec
	assert.Error(t, lc.WaitForReady(ctx, nil), "timeout failed")
}

func TestLogCheckerOk(t *testing.T) {
	// test regex
	rc := NewReadyChecker("test", &ReadySpec{StdoutLog: "count 2"})
	lc := rc.(*LogChecker)
	lc.logListener = make(chan string, 1)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	go func() {
		for i := 0; i < 3; i++ {
			lc.logListener <- fmt.Sprintf("count %d", i+1)
			time.Sleep(time.Millisecond * 200)
		}
	}()

	assert.NoError(t, lc.WaitForReady(ctx, nil), "count 2 found")
}

func TestLogCheckerRunnable(t *testing.T) {
	counter := &Runnable{
		Id:      "counter",
		Command: "for i in 3 2 1 111111; do echo count $i; sleep 0.1; done",
	}
	initRunnable(counter)

	ctx := context.Background()
	rc := NewReadyChecker("test", &ReadySpec{StdoutLog: "count 11+"})

	_ = rc.Init(ctx, counter)
	_ = counter.Cmd.Start()
	assert.NoError(t, rc.WaitForReady(ctx, counter))
}

func TestLogCheckerExit(t *testing.T) {
	counter := &Runnable{
		Id:      "counter",
		Command: "for i in 3 2 1; do echo count $i; sleep 0.1; done",
	}
	initRunnable(counter)

	ctx := context.Background()
	rc := NewReadyChecker("test", &ReadySpec{StdoutLog: "count 11+"})

	// counter process terminates before 'count 111'
	_ = rc.Init(ctx, counter)
	_ = counter.Cmd.Start()
	assert.Error(t, rc.WaitForReady(ctx, counter))
}

func TestLogCheckerTerminate(t *testing.T) {
	counter := &Runnable{
		Id:      "exit",
		Command: "exit 127",
	}
	initRunnable(counter)

	ctx := context.Background()
	rc := NewReadyChecker("test", &ReadySpec{StdoutLog: "count 11+"})

	// counter process terminates before 'count 111'
	_ = rc.Init(ctx, counter)
	_ = counter.Cmd.Start()
	assert.Error(t, rc.WaitForReady(ctx, counter))
	assert.Equal(t, 127, counter.Cmd.Status().Exit)
}

// TestHttpChecker tests th http ready checker for various scenario.
func TestHttpChecker(t *testing.T) {
	var testCases = []struct {
		name      string
		valid     bool
		readySpec *HttpReadySpec
	}{
		// Test cases
		{name: "no such domain", readySpec: &HttpReadySpec{
			URL: "http://no.such.domain",
		}, valid: false},
		{name: "200 ok", readySpec: &HttpReadySpec{
			URL:    "http://localhost:9933/status?q=200",
			Expect: &HttpReadyExpect{}, // default expected status 200
		}, valid: true},
		{name: "403 error", readySpec: &HttpReadySpec{
			URL:    "http://localhost:9933/status?q=403",
			Expect: &HttpReadyExpect{Status: 200},
		}, valid: false},
		{name: "301 moved", readySpec: &HttpReadySpec{
			URL:    "http://localhost:9933/status?q=301",
			Expect: &HttpReadyExpect{Status: 301},
		}, valid: true},
		{name: "200 json ok", readySpec: &HttpReadySpec{
			URL:    "http://localhost:9933/ready",
			Expect: &HttpReadyExpect{Status: 200, BodyRegex: `"ok"`},
		}, valid: true},
		{name: "200 json error", readySpec: &HttpReadySpec{
			URL:    "http://localhost:9933/error",
			Expect: &HttpReadyExpect{Status: 200, BodyRegex: `"ok"`},
		}, valid: false},
		{name: "200 header ok", readySpec: &HttpReadySpec{
			URL:    "http://localhost:9933/plain",
			Expect: &HttpReadyExpect{Headers: map[string]string{"content-type": "text/plain"}},
		}, valid: true},
		{name: "200 header error", readySpec: &HttpReadySpec{
			URL:    "http://localhost:9933/plain",
			Expect: &HttpReadyExpect{Headers: map[string]string{"content-type": "application/json"}},
		}, valid: false},
		{name: "200 timeout", readySpec: &HttpReadySpec{
			URL:    "http://localhost:9933/timeout",
			Expect: &HttpReadyExpect{Headers: map[string]string{"content-type": "text/plain"}},
			// this should fail due to timeout of 2 seconds set in this set.
		}, valid: false},
	}

	setupMockServer()

	// start the mock server.
	mockServer := &http.Server{Addr: "localhost:9933"}
	go func() {
		assert.Equal(t, http.ErrServerClosed, mockServer.ListenAndServe())
	}()

	ctx := context.TODO()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rc := NewReadyChecker("test", &ReadySpec{
				Timeout: Duration{time.Second * 5},
				Http:    tc.readySpec,
			})
			assert.NoError(t, rc.Init(ctx, nil))
			err := rc.WaitForReady(ctx, nil)
			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
	_ = mockServer.Shutdown(ctx)
}

func TestTcpCheckerOK(t *testing.T) {
	c := NewReadyChecker("tcp", &ReadySpec{
		Tcp: &TcpReadySpec{
			Host: "goproxy.io",
			Port: 80,
		},
	})

	assert.NoError(t, c.WaitForReady(context.TODO(), nil))
	assert.Equal(t, 0, c.(*TcpChecker).RetryCount())
}

func TestTcpCheckerNoConnect(t *testing.T) {
	c := NewReadyChecker("tcp", &ReadySpec{
		Tcp: &TcpReadySpec{
			Host: "goproxy.io",
			Port: 23, // telnet port is not reachable.
		},
		Timeout: Duration{time.Second},
	})

	assert.Error(t, c.WaitForReady(context.TODO(), nil))
}

func TestTcpCheckerRefused(t *testing.T) {
	c := NewReadyChecker("tcp", &ReadySpec{
		Tcp: &TcpReadySpec{
			Host: "localhost",
			Port: 1, // The port 1 is normally refused.
			// try twice
			Interval: Duration{time.Millisecond * 500},
		},
		Timeout: Duration{time.Second},
	})

	tc := c.(*TcpChecker)
	assert.Error(t, c.WaitForReady(context.TODO(), nil))
	assert.Equal(t, 2, tc.RetryCount())
}

func TestMySqlCheckOK(t *testing.T) {
	// Run only within CI
	if os.Getenv("CI_REPO_WORKSPACE") == "" {
		t.Skip("this runs only within the CI pipeline")
		return
	}

	{
		rc := NewReadyChecker("mysql", &ReadySpec{
			MySql: &MySqlReadySpec{
				Addr:     "localhost:3306",
				User:     "root",
				Table:    "hello",
				Database: "testdb",
			},
		})

		ctx := context.Background()
		assert.NoError(t, rc.Init(ctx, nil))
		err := rc.WaitForReady(ctx, nil)
		assert.NoError(t, err)
	}
}

func TestMySqlCheckFail(t *testing.T) {
	{
		rc := NewReadyChecker("mysql", &ReadySpec{
			Timeout: Duration{Duration: time.Second * 3},
			MySql: &MySqlReadySpec{
				Addr:     "localhost:1010",
				Table:    "hello",
				Database: "testdb",
				Interval: time.Second,
			},
		})

		ctx := context.Background()
		assert.NoError(t, rc.Init(ctx, nil))
		err := rc.WaitForReady(ctx, nil)
		assert.Error(t, err)
	}
}

func TestMongoCheckOK(t *testing.T) {
	// Run only within CI
	if os.Getenv("CI_REPO_WORKSPACE") == "" {
		t.Skip("this runs only within the CI pipeline")
		return
	}

	{
		rc := NewReadyChecker("mongodb", &ReadySpec{
			MongoDb: &MongoDbReadySpec{
				URI: "mongodb://mongo:27017/admin",
			},
		})

		ctx := context.Background()
		assert.NoError(t, rc.Init(ctx, nil))
		err := rc.WaitForReady(ctx, nil)
		assert.NoError(t, err)
	}
}

func TestMongoCheckFail(t *testing.T) {
	{
		rc := NewReadyChecker("mongodb", &ReadySpec{
			Timeout: Duration{Duration: time.Second * 3},
			MongoDb: &MongoDbReadySpec{
				URI: "mongodb://mongo:mongo@localhost:1010/admin",
			},
		})

		ctx := context.Background()
		assert.NoError(t, rc.Init(ctx, nil))
		err := rc.WaitForReady(ctx, nil)
		assert.Error(t, err)
	}
}

func TestHttpClient(t *testing.T) {
	setupMockServer()

	// start the mock server.
	mockServer := &http.Server{Addr: "localhost:9933"}
	go func() {
		assert.Equal(t, http.ErrServerClosed, mockServer.ListenAndServe())
	}()

	ctx := context.Background()

	cli := resty.New()
	rctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	// Must fail due to timeout of 1 sec
	_, err := cli.R().SetContext(rctx).Get("http://localhost:9933/timeout")
	assert.Error(t, err)

	_ = mockServer.Shutdown(ctx)
}

func TestShellChecker(t *testing.T) {
	var testCases = []struct {
		name      string
		valid     bool
		readySpec *ShellReadySpec
	}{
		// Test cases
		{name: "no such domain", readySpec: &ShellReadySpec{
			Command:  "curl -s -w '%{http_code}' http://no.such.domain | grep 200",
			Interval: Duration{time.Second * 3},
		}, valid: false},
		{name: "200 ready", readySpec: &ShellReadySpec{
			Command:  `curl -s -w '%{http_code}' http://localhost:9933/ready | grep "ok"`,
			Interval: Duration{time.Second},
		}, valid: true},
		{name: "200 timeout", readySpec: &ShellReadySpec{
			// This test fails because /timeout endpoint returns after 10 secs.
			Command:  `curl -s -w '%{http_code}' http://localhost:9933/timeout | grep "ok"`,
			Interval: Duration{time.Second * 2},
		}, valid: false},
	}

	setupMockServer()

	// start the mock server.
	mockServer := &http.Server{Addr: "localhost:9933"}
	go func() {
		assert.Equal(t, http.ErrServerClosed, mockServer.ListenAndServe())
	}()

	ctx := context.TODO()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rc := NewReadyChecker("test", &ReadySpec{
				Timeout: Duration{time.Second * 5},
				Shell:   tc.readySpec,
			})
			assert.NoError(t, rc.Init(ctx, nil))
			err := rc.WaitForReady(ctx, nil)
			log.Println(err)
			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}

	_ = mockServer.Shutdown(ctx)
}

// setupMockServer registers mock server endpoints.
func setupMockServer() {
	once.Do(func() {
		// Setup /ready and /error URL to mock ready checker.
		http.HandleFunc("/status", func(rw http.ResponseWriter, r *http.Request) {
			status := r.URL.Query().Get("q")
			code, err := strconv.Atoi(status)
			if err != nil || len(status) != 3 || status[0] == '0' {
				rw.WriteHeader(500)
			} else if code == 301 {
				rw.Header().Set("Location", "http://localhost:9933/ready")
				rw.WriteHeader(code)
			} else {
				rw.WriteHeader(code)
			}
		})
		// Setup /ready and /error URL to mock ready checker.
		http.HandleFunc("/ready", func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("content-type", "application/json")
			rw.WriteHeader(200)
			_, _ = rw.Write([]byte(`{"ready":"ok"}`))
		})
		http.HandleFunc("/error", func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("content-type", "application/json")
			rw.WriteHeader(403)
			_, _ = rw.Write([]byte(`{"ready":"error"}`))
		})
		http.HandleFunc("/plain", func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("content-type", "text/plain")
			rw.WriteHeader(200)
		})
		http.HandleFunc("/timeout", func(rw http.ResponseWriter, r *http.Request) {
			// takes 10 secs
			rw.Header().Set("content-type", "text/plain")
			// Wait for 10 secs or the request is closed by the client ...
			select {
			case <-time.After(time.Second * 10):
			case <-r.Context().Done():
			}
			rw.WriteHeader(200)
		})
	})
}
