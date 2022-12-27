// Package ps contains utility functions related to OS processes.
package ps

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pterm/pterm"
	"github.com/shirou/gopsutil/process"
)

type Process struct {
	Pid        int
	PPid       int
	Exe        string
	Cmdline    string
	WorkingDir string
	Children   []*Process
}

// dfs iterates the tree in the DFS order.
func (p *Process) dfs(depth int, op func(p *Process, depth int, pre bool) error) error {
	if err := op(p, depth, true); err != nil {
		return err
	}
	for _, child := range p.Children {
		if err := child.dfs(depth+1, op); err != nil {
			return err
		}
	}
	if err := op(p, depth, false); err != nil {
		return err
	}
	return nil
}

// Kill kills this process and its children.
func (p *Process) Kill() error {
	// TODO: implement me
	return nil
}

// Tree returns a human-readable tree of the process tree.
func (p *Process) Tree() string {
	w := bytes.Buffer{}
	_ = p.dfs(0, func(p *Process, depth int, pre bool) error {
		if pre {
			_, _ = fmt.Fprintf(&w, "%*s %s (%d)\n", depth, "  ", p.Exe, p.Pid)
		}
		return nil
	})

	return w.String()
}

// populateFrom fills Process from process.Process.
func (p *Process) populateFrom(proc *process.Process) {
	p.Pid = int(proc.Pid)
	ppid, _ := proc.Ppid()
	p.PPid = int(ppid)

	exe, _ := proc.Exe()
	p.Exe = filepath.Base(exe)

	wd, _ := proc.Cwd()
	p.WorkingDir = wd

	cmdline, _ := proc.Cmdline()
	p.Cmdline = cmdline
	if exe == "" {
		cmds := strings.Split(p.Cmdline, " ")
		if len(cmds) > 0 {
			p.Exe = filepath.Base(cmds[0])
		}
	}
}

// FindProcess finds the process with a given PID and returns simplified Process struct or error.
func FindProcess(pid int) (*Process, error) {
	if ok, err := process.PidExists(int32(pid)); err != nil {
		return nil, fmt.Errorf("pid not found: %w", err)
	} else if !ok {
		pterm.Warning.Printfln("no such PID found: %d", pid)
	}

	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return nil, fmt.Errorf("NewProcess error: %w", err)
	}

	root := Process{}
	root.populateFrom(proc)
	if err := buildProcessTree(&root, proc); err != nil {
		return nil, err
	}
	return &root, nil
}

// buildProcessTree searches through a given process's children and populates all children information.
func buildProcessTree(info *Process, proc *process.Process) error {
	children, err := proc.Children()
	if err != nil {
		if err == process.ErrorNoChildren {
			return nil
		}
		return fmt.Errorf("cannot find process children: %w", err)
	}
	for _, child := range children {
		childInfo := Process{}
		childInfo.populateFrom(child)
		if err := buildProcessTree(&childInfo, child); err != nil {
			return err
		}
		info.Children = append(info.Children, &childInfo)
	}
	return nil
}
