//go:build go1.18
// +build go1.18

package main

import (
	"log"
	"runtime/debug"

	"github.com/pterm/pterm"
)

func printVersion() {
	bi, _ := debug.ReadBuildInfo()
	var vcsCommit string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			vcsCommit = s.Value
		}
	}

	if vcsCommit != "" {
		log.Printf("Tok PM Version: %s (%s)", bi.Main.Version, vcsCommit)
	} else {
		log.Printf("Tok PM Version: %s", bi.Main.Version)
	}

	log.Println("Tok PM Manual:", pterm.Yellow("https://bytedance.feishu.cn/wiki/wikcnhyWgWiBCCHHyg2ddIfiRMd"))
}
