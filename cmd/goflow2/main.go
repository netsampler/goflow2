package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	_ "net/http/pprof"

	// import various formatters
	"github.com/netsampler/goflow2/format"
	_ "github.com/netsampler/goflow2/format/json"
	_ "github.com/netsampler/goflow2/format/protobuf"
	_ "github.com/netsampler/goflow2/format/text"

	// import various transports
	"github.com/netsampler/goflow2/transport"
	_ "github.com/netsampler/goflow2/transport/file"
	_ "github.com/netsampler/goflow2/transport/kafka"

	// core libraries
	"github.com/netsampler/goflow2/metrics"
	"github.com/netsampler/goflow2/utils"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

var (
	version    = ""
	buildinfos = ""
	AppVersion = "GoFlow2 " + version + " " + buildinfos

	ListenAddresses = flag.String("listen", "sflow://:6343,netflow://:2055", "listen addresses")

	LogLevel = flag.String("loglevel", "info", "Log level")
	LogFmt   = flag.String("logfmt", "normal", "Log formatter")

	Format    = flag.String("format", "json", fmt.Sprintf("Choose the format (available: %s)", strings.Join(format.GetFormats(), ", ")))
	Transport = flag.String("transport", "file", fmt.Sprintf("Choose the transport (available: %s)", strings.Join(transport.GetTransports(), ", ")))

	MetricsAddr = flag.String("metrics.addr", ":8080", "Metrics address")

	TemplatePath = flag.String("templates.path", "/templates", "NetFlow/IPFIX templates list")

	MappingFile = flag.String("mapping", "", "Configuration file for custom mappings")

	Version = flag.Bool("v", false, "Print version")
)

func httpServer( /*state *utils.StateNetFlow*/ ) {
	http.Handle("/metrics", promhttp.Handler())
	//http.HandleFunc(*TemplatePath, state.ServeHTTPTemplates)
	log.Fatal(http.ListenAndServe(*MetricsAddr, nil))
}

func main() {
	flag.Parse()

	if *Version {
		fmt.Println(AppVersion)
		os.Exit(0)
	}

	lvl, _ := log.ParseLevel(*LogLevel)
	log.SetLevel(lvl)

	var config utils.ProducerConfig
	if *MappingFile != "" {
		f, err := os.Open(*MappingFile)
		if err != nil {
			log.Fatal(err)
		}
		config, err = utils.LoadMapping(f)
		f.Close()
		if err != nil {
			log.Fatal(err)
		}
	}

	ctx := context.Background()

	formatter, err := format.FindFormat(ctx, *Format)
	if err != nil {
		log.Fatal(err)
	}

	transporter, err := transport.FindTransport(ctx, *Transport)
	if err != nil {
		log.Fatal(err)
	}
	defer transporter.Close(ctx)

	switch *LogFmt {
	case "json":
		log.SetFormatter(&log.JSONFormatter{})
	}

	log.Info("Starting GoFlow2")
	go httpServer()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	var receivers []*utils.UDPReceiver

	wg := &sync.WaitGroup{}
	q := make(chan bool)
	for _, listenAddress := range strings.Split(*ListenAddresses, ",") {
		listenAddrUrl, err := url.Parse(listenAddress)
		if err != nil {
			log.Fatal(err)
		}
		numSockets := 1
		if listenAddrUrl.Query().Has("count") {
			if numSocketsTmp, err := strconv.ParseUint(listenAddrUrl.Query().Get("count"), 10, 64); err != nil {
				log.Fatal(err)
			} else {
				numSockets = int(numSocketsTmp)
			}
		}
		if numSockets == 0 {
			numSockets = 1
		}

		hostname := listenAddrUrl.Hostname()
		port, err := strconv.ParseUint(listenAddrUrl.Port(), 10, 64)
		if err != nil {
			log.Errorf("Port %s could not be converted to integer", listenAddrUrl.Port())
			return
		}

		logFields := log.Fields{
			"scheme":   listenAddrUrl.Scheme,
			"hostname": hostname,
			"port":     port,
			"count":    numSockets,
		}
		l := log.WithFields(logFields)

		l.Info("Starting collection")

		cfg := &utils.UDPReceiverConfig{
			Sockets: numSockets,
		}
		recv := utils.NewUDPReceiver(cfg)

		var decodeFunc utils.DecoderFunc
		if listenAddrUrl.Scheme == "sflow" {
			state := &utils.StateSFlow{
				Format:    formatter,
				Transport: transporter,
				Producer:  metrics.PromProducerDefaultWrapper(),
				Logger:    log.StandardLogger(),
				Config:    config,
			}
			decodeFunc = metrics.PromDecoderWrapper(state.DecodeFlow, listenAddrUrl.Scheme)
			//err = sSFlow.FlowRoutine(*Workers, hostname, int(port), *ReusePort)
		} else if listenAddrUrl.Scheme == "netflow" {
			state := &utils.StateNetFlow{
				Format:    formatter,
				Transport: transporter,
				Producer:  metrics.PromProducerDefaultWrapper(),
				Logger:    log.StandardLogger(),
				Config:    config,
			}
			decodeFunc = metrics.PromDecoderWrapper(state.DecodeFlow, listenAddrUrl.Scheme)
			//err = sNF.FlowRoutine(*Workers, hostname, int(port), *ReusePort)
		} else {
			l.Errorf("scheme %s does not exist", listenAddrUrl.Scheme)
			return
		}

		if err := recv.Start(hostname, int(port), decodeFunc); err != nil {
			l.Fatal(err)
		} else {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-q:
						return
					case err := <-recv.Errors():
						l.WithError(err).Error("error")
					}
				}
			}()
			receivers = append(receivers, recv)
		}
	}

	<-c

	for _, recv := range receivers {
		recv.Stop()
	}
	close(q)
	wg.Wait()

}
