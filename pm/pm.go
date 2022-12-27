package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/pterm/pterm"
	"github.com/telnet2/pm"
	"github.com/telnet2/pm/shutdown"
)

func main() {
	log.SetPrefix(pterm.Green("[PMAN] "))
	log.SetFlags(log.Lmsgprefix)
	log.SetOutput(os.Stdout)

	confFile := "pm.yaml"
	printConfig := false

	flag.StringVar(&confFile, "conf", "pm.yaml", "Config file for PM")
	flag.BoolVar(&printConfig, "print-config", false, "Print configuration only")
	flag.Parse()

	printVersion()

	ctx := context.TODO()
	cfg, err := pm.LoadYamlFile(ctx, confFile)
	if err != nil {
		log.Fatalf("fail to read config file: %v", err)
	}

	if printConfig {
		fmt.Println(cfg.String())
		return
	}

	pm.GoFunc(func() {
		shutdown.ListenUpToCount(5, os.Interrupt)
	})

	pman := pm.NewProcMan()
	err = pman.AddConfig(cfg)
	if err != nil {
		log.Fatalf("fail to read config file: %v", err)
	}

	ch := pman.SubscribeEvent("*", map[string]struct{}{"start": {}, "ready": {}, "done": {}})
	pm.GoFunc(func() {
		for e := range ch {
			log.Printf("[%s] %s", e.Id, e.Event)
		}
	})

	err = pman.Start(ctx)
	if err != nil {
		log.Fatalf("fail to start: %v", err)
	} else {
		args := flag.Args()
		if len(args) > 0 {
			exec := exec.Command("/bin/bash", "-c", args[0])
			exec.Stdout = os.Stdout
			exec.Stderr = os.Stderr
			exec.Env = os.Environ()
			shutdown.Add(func() {
				// kill the process if an interrupt signal is received.
				if exec.Process != nil {
					log.Printf("killing process: %d", exec.Process.Pid)
					_ = exec.Process.Kill()
				}
			})
			log.Printf("Running %s", args[0])
			_ = exec.Run()
		} else {
			pman.WaitDone()
		}
	}
	pman.Shutdown(nil)
}
