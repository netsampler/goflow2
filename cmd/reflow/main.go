package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"

	"github.com/netsampler/goflow2/v3/pkg/reflow/app"
	"github.com/netsampler/goflow2/v3/pkg/reflow/buildprofile"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/encode"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source"
	"gopkg.in/yaml.v3"
)

var (
	version    = ""
	buildinfos = ""
	appVersion = "ReFlow " + version + " " + buildinfos
)

func main() {
	cfg, versionFlag := config.BindFlags(flag.CommandLine)
	cpuProfile := flag.String("cpuprofile", "", "Write a CPU profile to this file; finalized on shutdown")
	if err := flag.CommandLine.Parse(config.NormalizeAggregateArgs(os.Args[1:])); err != nil {
		log.Fatal(err)
	}

	if *versionFlag {
		fmt.Println(appVersion, buildprofile.Summary(), source.SDKVersions())
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
		raw, err := encode.GenerateProtobufDefinition(loadedCfg.Encoder.Protobuf)
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

	var profile *os.File
	if *cpuProfile != "" {
		profile, err = os.OpenFile(*cpuProfile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			log.Fatal(err)
		}
		if err := pprof.StartCPUProfile(profile); err != nil {
			_ = profile.Close()
			log.Fatal(err)
		}
	}
	runErr := application.Run(ctx)
	if profile != nil {
		pprof.StopCPUProfile()
		if err := profile.Close(); err != nil && runErr == nil {
			runErr = fmt.Errorf("close CPU profile: %w", err)
		}
	}
	if runErr != nil {
		slog.Error("application error", slog.String("error", runErr.Error()))
		os.Exit(1)
	}
}
