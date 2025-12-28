// Command goflow2 receives NetFlow/sFlow packets and exports flow messages.
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
	"time"

	"github.com/netsampler/goflow2/v2/pkg/goflow2/app"
	"github.com/netsampler/goflow2/v2/pkg/goflow2/config"

	_ "github.com/netsampler/goflow2/v2/format/binary"
	_ "github.com/netsampler/goflow2/v2/format/json"
	_ "github.com/netsampler/goflow2/v2/format/text"
	_ "github.com/netsampler/goflow2/v2/transport/file"
	_ "github.com/netsampler/goflow2/v2/transport/kafka"
)

var (
	version    = ""
	buildinfos = ""
	// AppVersion is a display string for the current build.
	AppVersion = "GoFlow2 " + version + " " + buildinfos

	Version = flag.Bool("v", false, "Print version")
)

func main() {
	cfg := config.BindFlags(flag.CommandLine)
	flag.Parse()

	if *Version {
		fmt.Println(AppVersion)
		os.Exit(0)
	}

	application, err := app.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	if err := application.Start(); err != nil {
		slog.Error("failed to start application", slog.String("error", err.Error()))
		os.Exit(1)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	select {
	case <-c:
	case err := <-application.Wait():
		if err != nil {
			slog.Error("HTTP server error", slog.String("error", err.Error()))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	application.Shutdown(ctx)
	cancel()
}
