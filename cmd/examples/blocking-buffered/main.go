package main

import (
	"fmt"

	"github.com/telnet2/pm/cmd"
)

func main() {
	// Create Cmd, buffered output
	envCmd := cmd.NewCmd("count", "for x in {0..3}; do echo $x; done")

	// Run and wait for Cmd to return Status
	status := <-envCmd.Start()

	// Print each line of STDOUT from Cmd
	for _, line := range status.Stdout {
		fmt.Println(line)
	}
}
