// Command meshd is the mesh-msg desktop daemon (client, gateway, bridge).
//
//	meshd                         # uses ~/.config/meshd/config.toml, ~/.local/share/meshd
//	meshd -config c.toml -data d  # custom locations
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Aryalleh/mesh-msg/desktop/daemon"
)

var version = "0.0.0-dev" // set by the release build (-X main.version=…)

func main() {
	defCfg, defData := daemon.DefaultPaths()
	cfgPath := flag.String("config", defCfg, "config file (created with defaults if missing)")
	dataDir := flag.String("data", defData, "data directory (identity, databases, socket)")
	verbose := flag.Bool("v", false, "verbose logging")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println("meshd", version)
		return
	}

	cfg, err := daemon.LoadConfig(*cfgPath, *dataDir)
	if err != nil {
		log.Fatal(err)
	}
	if *verbose {
		cfg.Logf = log.Printf
	}
	if daemon.Running(cfg.DataDir) {
		log.Fatal(daemon.ErrRunning)
	}
	d, err := daemon.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	d.SetVersion(version)
	id := d.Node().Identity()
	log.Printf("meshd %s — identity %s", version, id.Hash())
	log.Printf("config %s, data %s", *cfgPath, *dataDir)
	if u := d.UIURL(); u != "" {
		log.Printf("web UI: run `mesh ui` to open it")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := d.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
