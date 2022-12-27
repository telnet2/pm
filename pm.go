// Package pm provides a process manager based on github.com/telnet2/pm/cmd.
// The `pm` manages the execution of multiple processes in the order of dependencies.
package pm

import (
	"context"
	"sync"
	"time"

	"github.com/telnet2/pm/cmd"
	"github.com/telnet2/pm/envmap"
)

type EventMap = map[string]struct{}

type ProcEvent struct {
	Id       string
	Source   *Runnable
	Event    string
	ExitCode int
}

// ReadyChecker is reponsible for waiting a runnable is ready.
// We need to break the dependency to `Runnable`.
// Otherwise, a ReadyChecker must be created in this package due to circular dependency.
type ReadyChecker interface {
	Init(context.Context, *Runnable) error // BeforeStart  is called before a given runnable starts.

	// WaitForReady is called after a given runnable starts.
	// It shouldn't return if the runnable is not ready.
	WaitForReady(context.Context, *Runnable) error
}

// Runnable is a configuration of an executable.
type Runnable struct {
	Id             string     `yaml:"id" json:"id,omitempty"`                                // Id must be unique within the ProcMan.
	Command        string     `yaml:"command" json:"command,omitempty" envexp:""`            // Command to execute via bash
	CommandEx      string     `yaml:"command_ex,omitempty" json:"commandEx,omitempty"`       // Command without env expansaion. CommandEx has priority.
	Env            []string   `yaml:"environment,omitempty" json:"environment,omitempty"`    // Env is a list of env vars in the form of NAME=VALUE
	EnvFile        string     `yaml:"env_file,omitempty" json:"envFile,omitempty" envexp:""` // EnvFile to load env vars
	WorkDir        string     `yaml:"work_dir,omitempty" json:"workDir,omitempty" envexp:""` // Working directory
	LogDir         string     `yaml:"log_dir,omitempty" json:"logDir,omitempty" envexp:""`   // If LogDir is set, then the ${LogDir}/${id}.stdout.log and ${LogDir}/${id}.stderr.log files are created and stored.
	DependsOn      []string   `yaml:"depends_on,omitempty" json:"dependsOn,omitempty"`       // DependsOn is a list of runnables to run before this
	ReadyCondition *ReadySpec `yaml:"ready_condition,omitempty" json:"readyCheck,omitempty"` // ReadyCondition is the condition to determine if the process is ready

	Cmd   *cmd.Cmd     `yaml:"-" json:"-"` // Cmd is go's process execution object
	Ready ReadyChecker `yaml:"-" json:"-"`

	ConfDir string        `yaml:"-" json:"-"` // the origin of this file
	envMap  envmap.EnvMap `yaml:"-" json:"-"`

	l *sync.Mutex `yaml:"-" json:"-"` // lock
}

// GetRuntimeEnvs returns env vars of this runnable.
func (r *Runnable) GetRuntimeEnvs() []string {
	if r.envMap == nil {
		ExpandEnvs(r)
	}
	return r.envMap.AsString()
}

// GetCommand returns its command to run either from CommandRaw or Command.
func (r *Runnable) GetCommand() string {
	if r.CommandEx == "" {
		return r.Command
	}
	return r.CommandEx
}

// CloneCmd clones the underlying cmd.
func (r *Runnable) CloneCmd() {
	defer r.l.Unlock()
	r.l.Lock()
	if r.Cmd == nil {
		return
	}
	r.Cmd = r.Cmd.Clone()
}

// IsDone returns the underlying cmd's IsDone()
func (r *Runnable) IsDone() bool {
	defer r.l.Unlock()
	r.l.Lock()
	if r.Cmd == nil {
		return false
	}
	return r.Cmd.IsDone()
}

// ReadySpec provides a simple ready checker
type ReadySpec struct {
	StdoutLog string            `yaml:"stdout_message,omitempty" json:"stdoutMessage,omitempty"` // log is a regex to find from stdout/stderr logs
	Http      *HttpReadySpec    `yaml:"http,omitempty" json:"http,omitempty"`                    // http defines the ready condition via http protocol
	MySql     *MySqlReadySpec   `yaml:"mysql,omitempty" json:"mysql,omitempty"`                  // http defines the ready condition via http protocol
	MongoDb   *MongoDbReadySpec `yaml:"mongo_db,omitempty" json:"mongoDb,omitempty"`             // http defines the ready condition via http protocol
	Shell     *ShellReadySpec   `yaml:"shell,omitempty" json:"shell,omitempty"`
	Tcp       *TcpReadySpec     `yaml:"tcp,omitempty" json:"tcp,omitempty"`
	Timeout   Duration          `yaml:"timeout,omitempty" json:"timeout,omitempty" default:"1m"` // timeout (default 1min)
}

type TcpReadySpec struct {
	Host     string   `yaml:"host,omitempty" json:"host,omitempty" default:"localhost"`
	Port     int      `yaml:"port,omitempty" json:"port,omitempty"`
	Interval Duration `yaml:"interval,omitempty" json:"interval,omitempty" default:"3s"` // polling interval (default 3s)
}

type ShellReadySpec struct {
	Command  string   `yaml:"command,omitempty" json:"command,omitempty" envexp:""`      // shell command to run
	Interval Duration `yaml:"interval,omitempty" json:"interval,omitempty" default:"3s"` // polling interval (default 3s)
}

type HttpReadyExpect struct {
	Status    int               `yaml:"status,omitempty" json:"status,omitempty"`
	Headers   map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	BodyRegex string            `yaml:"body_regex,omitempty" json:"bodyRegex,omitempty"`
}

// HttpReadySpec defines how to determine the readiness of the url.
// If `expect` field is not specified, 200 OK is considered to be ready.
type HttpReadySpec struct {
	URL      string           `yaml:"url" json:"url,omitempty"  validate:"required" envexp:""`
	Interval time.Duration    `yaml:"interval,omitempty" json:"interval,omitempty" default:"3s"` // polling interval (default 3s)
	Expect   *HttpReadyExpect `yaml:"expect" json:"expect,omitempty"`
}

// MySqlReadySpec defines how to determine the readiness of a mysql connection.
type MySqlReadySpec struct {
	Network  string        `yaml:"network,omitempty" json:"network,omitempty" default:"tcp"`
	Addr     string        `yaml:"addr" json:"addr" envexp:""` // Suppor TCP only
	Database string        `yaml:"database" json:"database" envexp:""`
	User     string        `yaml:"user" json:"user" default:"root" envexp:""`
	Password string        `yaml:"password,omitempty" json:"password,omitempty" envexp:""`
	Table    string        `yaml:"table,omitempty" json:"table,omitempty" envexp:""`
	Interval time.Duration `yaml:"interval,omitempty" json:"interval,omitempty" default:"3s"` // polling interval (default 3s)
}

// MongoDbReadySpec defines how to determine the readiness of a mysql connection.
type MongoDbReadySpec struct {
	URI      string        `yaml:"uri" json:"uri" default:"mongo://mongo:mongo@localhost:27017/admin" envexp:""`
	Interval time.Duration `yaml:"interval,omitempty" json:"interval,omitempty" default:"3s"` // polling interval (default 3s)
}
