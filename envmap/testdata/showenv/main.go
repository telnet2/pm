package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	// Test code to check env var
	envName := flag.String("env", "", "environment variable name to print")
	flag.Parse()

	if *envName == "" {
		return
	}
	fmt.Print(os.Getenv(*envName))
}
