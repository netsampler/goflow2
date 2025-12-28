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

	"github.com/netsampler/goflow2/v3/pkg/goflow2/app"
	"github.com/netsampler/goflow2/v3/pkg/goflow2/config"

	_ "github.com/netsampler/goflow2/v3/format/binary"
	_ "github.com/netsampler/goflow2/v3/format/json"
	_ "github.com/netsampler/goflow2/v3/format/text"
	_ "github.com/netsampler/goflow2/v3/transport/file"
	_ "github.com/netsampler/goflow2/v3/transport/kafka"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := application.Run(ctx); err != nil {
		slog.Error("application error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
