### Stop Order

- Process exits or is terminated
- 
### Modification

- Always run with bash -c
    - Helps to run bash commands (e.g., echo, sleep, ...)
    - Helps to use EnvVars in .bashrc
- Add `StdoutPub` and `StderrPub` to collect logs
- Add log file writer
- Use Name as identifier
- Add `OnStop()` func to call when the proc ends


### Go-Cmd Advantages

- Kill the process group
- Status checking (e.g., exit status)
- Thoroughly tested
- Safe cleanup
