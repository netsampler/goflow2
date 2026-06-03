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

	"github.com/netsampler/goflow2/v3/pkg/reflow/app"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/encode"
	"gopkg.in/yaml.v3"
)

var (
	version    = ""
	buildinfos = ""
	appVersion = "ReFlow " + version + " " + buildinfos
)

func main() {
	cfg, versionFlag := config.BindFlags(flag.CommandLine)
	if err := flag.CommandLine.Parse(config.NormalizeAggregateArgs(os.Args[1:])); err != nil {
		log.Fatal(err)
	}

	if *versionFlag {
		fmt.Println(appVersion)
		os.Exit(0)
	}
	if cfg.ListOptions {
		fmt.Print(config.HelperOptionsText())
		return
	}

	loadedCfg, generated, err := config.LoadFromFlags(cfg)
	if err != nil {
		log.Fatal(err)
	}
	loadedCfg.LogLevel = cfg.LogLevel
	loadedCfg.LogFormat = cfg.LogFormat

	if cfg.GenConf {
		if !generated {
			log.Fatal("-genconf requires generated config mode; omit -config")
		}
		raw, err := yaml.Marshal(loadedCfg)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := os.Stdout.Write(raw); err != nil {
			log.Fatal(err)
		}
		return
	}
	if cfg.GenProto {
		raw, err := encode.GenerateProtobufDefinition(loadedCfg.Encoder.Protobuf.Flavor)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Print(raw)
		return
	}

	// App.New wires the full runtime graph from the loaded config.
	application, err := app.New(loadedCfg)
	if err != nil {
		log.Fatal(err)
	}

	// ReFlow exits cleanly on Ctrl-C or SIGTERM so downstream flush paths run.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := application.Run(ctx); err != nil {
		slog.Error("application error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
