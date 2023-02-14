package main

import (
	"fmt"
	"os"

	"github.com/telnet2/pm/term"
)

func main() {
	if err := term.ExecSSH(os.Args[1:]); err != nil {
		fmt.Println(err)
	}
}
