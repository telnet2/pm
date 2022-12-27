//go:build !go1.18
// +build !go1.18

package main

import (
	"log"
	"runtime/debug"

	"github.com/pterm/pterm"
)

func printVersion() {
	bi, _ := debug.ReadBuildInfo()
	log.Printf("Tok PM Version: %s", bi.Main.Version)
	log.Println("Tok PM Manual:", pterm.Yellow("https://bytedance.feishu.cn/wiki/wikcnhyWgWiBCCHHyg2ddIfiRMd"))
}
