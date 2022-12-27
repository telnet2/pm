# Process Manager

## SYNOPSIS

The `pm` package can be used in two ways: as a command line or as an SDK.

**Command Line Usage**

In a command line, simply gives a procman config file. 
It starts each process in the config one by one in the order of their dependencies.

```shell
go install github.com/telnet2/pm/pm@latest
pm -conf pm.yaml "some command"
```

If an argument is given, `pm` starts up processes defined in the `pm.yaml`, executes the command given, and then stops all processes in the config.


**SDK Usage**

`ProcMan` is used as a library to manage process execution and lifecycle events.
It can start, restart, and stop individual processes.

```go
import "github.com/telnet2/pm"

pman := pm.NewProcMan()
cfg, err := pm.LoadYamlFile("pm.yaml")
if err := nil {
  log.Fatal(err)
}
pm.AddConfig(pman)

ctx := context.TODO()
err = pm.Start(ctx)
if err != nil {
  log.Fatalf("fail to start: %v", err)
}

go func() {
  // in some other go-routine ...
  err := pm.Restart("<process-id>")  // restart the process
  err = pm.Stop("<process-id>")
}()

pm.WaitDone()
pm.Shutdown(nil)
```

## DESCRIPTION

### Project File

```yaml
services:
  task1:
    command: "for x in {0..3}; do echo $x; sleep 1; done"
    ready_condition:
      stdout_message: "3"
  task2:
    command: echo "ls done"
    depends_on:
      - task1
  task3:
    command: env
    environment:
      - A=B
      - C=D
    env_file: ./config_test.env # relative to this config file or the conf dir found in the context
```

## COMPONENTS


### `pubsub` Package

A publisher-subscriber pattern for go-routines. 
This `pubsub` package is used to deliver events and logs from a process to other logics.

A `Publisher` is created to manage its `Subscriber`s. A subscriber is simply a channel that receives messages from the publisher. Compared to using a simple `channel`, this package has following advantages.

- delivery to multiple listeners / subscribers
- lazy joining to publishers
- easy to use
- publishing timeout
- receiving message conditionally
- all subscriber channels are closed when publisher is closed.

Instead of exchanging `interface{}`, a few publisher types are created such as `StringPublisher`, `ProcEventPublisher`.

**USAGE**
```go
// creates a publisher with 1s timeout and the buffer size of 10 for subscribers.
pub := NewStringPublisher(time.Second, 10)

// spawn a go-routine A
var sub1 chan string = pub.Subscribe()
for s := range sub1 {
    // process message s
}
// exits the for-loop when the sub1 is closed 

// in another go-routine B
sub2 := pub.Subscribe()
for s := range sub2 {
    // receive the same messages with sub1
}
```

### `cmd` Package

`cmd` package provides powerful control over a process lifecycle and its outputs (stdout/stderr). Compared to `exec.Command`, it helps to obtain the exit status correctly, kills the process group instead of killing the executed process only, streams the stdout/stderr outputs to channels, and provides event mechanism to monitor the process lifecycle events (start, running, stopped).

This `cmd` package is originally copied from https://github.com/go-cmd/cmd.
It has more customization adding `LogDir` to store outputs as log files conveiniently, using the above `pubsub` pattern in publishing stdout/stderr outputs, and executing a command via `sh -c` or `bash -c`. Since it is highly integrated with `procman`, we expect more customization to come to support future features.

**USAGE**

```go
count = cmd.NewCmdOptions(
			cmd.Options{Streaming: true},
			"count",
			"for i in {0..3}; do echo $i; done",
		)
count.Dir = runnable.Dir
count.Env = runnable.Env
count.LogDir = "/tmp/count_log_dir"

logCh := count.StdoutPub.Subscribe()

// start the count process asynchronously
_ = count.Start()

// print stdout of count
for l := range logCh {
    fmt.Println(l)
}
// process count is done

// Print out the exit code
fmt.Println("Exit:", count.Status().Exit)
```

### `Executor`

### `ReadyChecker`

We support two types of ready checkers. 
`ReadyChecker` implements various ready checkers. 

How to implement a custom ready checker?



## TODO

- [x] Star / restart / stop shell commands.
- [x] Launch in the order of the dependencies.
    - [ ] All stops if a dependent service fails to launch
- [ ] Support various ready checker.
    - [x] Log message
    - [x] Http query
    - [ ] File tail
    - MySQL database / table check
- [x] Configuration to describe a set of commands to run.
- [ ] Can run as a daemon.
- [ ] Can be used as an SDK.
    - [x] Monitoring logs using Pubsub pattern.
    - [x] Monitoring command status using Pubsub pattern.
