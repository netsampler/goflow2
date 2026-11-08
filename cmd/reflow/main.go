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
	"sync/atomic"
	"syscall"
	"time"

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
	timingEvery := flag.Uint64("metrics-timing-every", 16, "With OTLP metrics, time about one in N calls and all flushes (0 disables timing)")
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

	// ReFlow exits cleanly on Ctrl-C or SIGTERM so downstream flush paths run.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runApplication(ctx, loadedCfg, *cpuProfile, *timingEvery); err != nil {
		slog.Error("application error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func runApplication(ctx context.Context, cfg *config.Config, cpuProfile string, timingEvery uint64) (runErr error) {
	provider, err := newMeterProvider(ctx)
	if err != nil {
		return fmt.Errorf("init metrics export: %w", err)
	}
	var application *app.App
	if provider != nil {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if err := provider.Shutdown(shutdownCtx); err != nil {
				slog.Warn("metrics shutdown failed", "error", err)
			}
		}()
		var observed atomic.Pointer[app.App]
		if err := registerMetrics(provider, &observed, timingEvery > 0); err != nil {
			return fmt.Errorf("register metrics: %w", err)
		}
		application, err = app.NewWithStats(cfg, app.StatsOptions{TimingEvery: timingEvery})
		if err == nil {
			observed.Store(application)
		}
	} else {
		application, err = app.New(cfg)
	}
	if err != nil {
		return err
	}
	if cpuProfile != "" {
		profile, err := os.OpenFile(cpuProfile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if err := pprof.StartCPUProfile(profile); err != nil {
			_ = profile.Close()
			return err
		}
		defer func() {
			pprof.StopCPUProfile()
			if err := profile.Close(); err != nil && runErr == nil {
				runErr = fmt.Errorf("close CPU profile: %w", err)
			}
		}()
	}
	return application.Run(ctx)
}
