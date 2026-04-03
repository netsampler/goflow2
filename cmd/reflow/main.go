package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/netsampler/goflow2/v3/internal/reflow/app"
	"github.com/netsampler/goflow2/v3/internal/reflow/config"
)

var (
	version    = ""
	buildinfos = ""
	appVersion = "ReFlow " + version + " " + buildinfos
)

func main() {
	cfg, versionFlag := config.BindFlags(flag.CommandLine)
	flag.Parse()

	if *versionFlag {
		fmt.Println(appVersion)
		os.Exit(0)
	}

	loadedCfg, err := config.Load(cfg.ConfigPath)
	if err != nil {
		log.Fatal(err)
	}
	loadedCfg.LogLevel = cfg.LogLevel
	loadedCfg.LogFormat = cfg.LogFormat

	application, err := app.New(loadedCfg)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := application.Run(ctx); err != nil {
		slog.Error("application error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
