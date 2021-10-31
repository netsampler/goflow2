package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	// import various formatters
	"github.com/netsampler/goflow2/format"
	_ "github.com/netsampler/goflow2/format/json"
	_ "github.com/netsampler/goflow2/format/protobuf"
	_ "github.com/netsampler/goflow2/format/text"

	// import various transports
	"github.com/netsampler/goflow2/transport"
	_ "github.com/netsampler/goflow2/transport/file"
	_ "github.com/netsampler/goflow2/transport/kafka"

	// import various NetFlow/IPFIX templates
	"github.com/netsampler/goflow2/decoders/netflow/templates"
	_ "github.com/netsampler/goflow2/decoders/netflow/templates/memory"

	"github.com/netsampler/goflow2/utils"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

var (
	version    = ""
	buildinfos = ""
	AppVersion = "GoFlow2 " + version + " " + buildinfos

	ReusePort       = flag.Bool("reuseport", false, "Enable so_reuseport")
	ListenAddresses = flag.String("listen", "sflow://:6343,netflow://:2055", "listen addresses")

	Workers  = flag.Int("workers", 1, "Number of workers per collector")
	LogLevel = flag.String("loglevel", "info", "Log level")
	LogFmt   = flag.String("logfmt", "normal", "Log formatter")

	NetFlowTemplates = flag.String("netflow.templates", "memory", fmt.Sprintf("Choose the format (available: %s)", strings.Join(templates.GetTemplates(), ", ")))

	Format    = flag.String("format", "json", fmt.Sprintf("Choose the format (available: %s)", strings.Join(format.GetFormats(), ", ")))
	Transport = flag.String("transport", "file", fmt.Sprintf("Choose the transport (available: %s)", strings.Join(transport.GetTransports(), ", ")))

	MetricsAddr = flag.String("metrics.addr", ":8080", "Metrics address")
	MetricsPath = flag.String("metrics.path", "/metrics", "Metrics path")

	TemplatePath = flag.String("templates.path", "/templates", "NetFlow/IPFIX templates list")

	MappingFile = flag.String("mapping", "", "Configuration file for custom mappings")

	Version = flag.Bool("v", false, "Print version")
)

func httpServer( /*state *utils.StateNetFlow*/ ) {
	http.Handle(*MetricsPath, promhttp.Handler())
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
	//go httpServer(sNF)

	wg := &sync.WaitGroup{}

	for _, listenAddress := range strings.Split(*ListenAddresses, ",") {
		wg.Add(1)
		go func(listenAddress string) {
			defer wg.Done()
			listenAddrUrl, err := url.Parse(listenAddress)
			if err != nil {
				log.Fatal(err)
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
			}

			log.WithFields(logFields).Info("Starting collection")

			if listenAddrUrl.Scheme == "sflow" {
				sSFlow := utils.NewStateSFlow()
				sSFlow.Format = formatter
				sSFlow.Transport = transporter
				sSFlow.Logger = log.StandardLogger()
				sSFlow.Config = config

				err = sSFlow.FlowRoutine(*Workers, hostname, int(port), *ReusePort)
			} else if listenAddrUrl.Scheme == "netflow" {
				templateSystem, err := templates.FindTemplateSystem(ctx, *NetFlowTemplates)
				if err != nil {
					log.Fatal(err)
				}
				defer templateSystem.Close(ctx)

				sNF := utils.NewStateNetFlow()
				sNF.Format = formatter
				sNF.Transport = transporter
				sNF.Logger = log.StandardLogger()
				sNF.Config = config
				sNF.TemplateSystem = templateSystem

				err = sNF.FlowRoutine(*Workers, hostname, int(port), *ReusePort)
			} else if listenAddrUrl.Scheme == "nfl" {
				sNFL := utils.NewStateNFLegacy()
				sNFL.Format = formatter
				sNFL.Transport = transporter
				sNFL.Logger = log.StandardLogger()

				err = sNFL.FlowRoutine(*Workers, hostname, int(port), *ReusePort)
			} else {
				log.Errorf("scheme %s does not exist", listenAddrUrl.Scheme)
				return
			}

			if err != nil {
				log.WithFields(logFields).Fatal(err)
			}

		}(listenAddress)

	}

	wg.Wait()
}
