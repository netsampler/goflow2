package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	_ "net/http/pprof"

	// decoders
	"github.com/netsampler/goflow2/decoders/netflow"

	// various formatters
	"github.com/netsampler/goflow2/format"
	_ "github.com/netsampler/goflow2/format/binary"
	_ "github.com/netsampler/goflow2/format/json"
	_ "github.com/netsampler/goflow2/format/text"

	// various transports
	"github.com/netsampler/goflow2/transport"
	_ "github.com/netsampler/goflow2/transport/file"
	_ "github.com/netsampler/goflow2/transport/kafka"

	// various producers
	"github.com/netsampler/goflow2/producer"
	"github.com/netsampler/goflow2/producer/proto"
	"github.com/netsampler/goflow2/producer/raw"

	// core libraries
	"github.com/netsampler/goflow2/metrics"
	"github.com/netsampler/goflow2/utils"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

var (
	version    = ""
	buildinfos = ""
	AppVersion = "GoFlow2 " + version + " " + buildinfos

	ListenAddresses = flag.String("listen", "sflow://:6343,netflow://:2055", "listen addresses")

	LogLevel = flag.String("loglevel", "info", "Log level")
	LogFmt   = flag.String("logfmt", "normal", "Log formatter")

	Produce   = flag.String("produce", "sample", "Producer method (sample or raw)")
	Format    = flag.String("format", "json", fmt.Sprintf("Choose the format (available: %s)", strings.Join(format.GetFormats(), ", ")))
	Transport = flag.String("transport", "file", fmt.Sprintf("Choose the transport (available: %s)", strings.Join(transport.GetTransports(), ", ")))

	MetricsAddr = flag.String("metrics.addr", ":8080", "Metrics address")

	TemplatePath = flag.String("templates.path", "/templates", "NetFlow/IPFIX templates list")

	MappingFile = flag.String("mapping", "", "Configuration file for custom mappings")

	Version = flag.Bool("v", false, "Print version")

	producerOptions = map[string]bool{
		"sample": true,
		"raw":    true,
	}
)

func httpServer( /*state *utils.StateNetFlow*/ ) {
	http.Handle("/metrics", promhttp.Handler())
	//http.HandleFunc(*TemplatePath, state.ServeHTTPTemplates)
	log.Fatal(http.ListenAndServe(*MetricsAddr, nil))
}

func LoadMapping(f io.Reader) (*protoproducer.ProducerConfig, error) {
	config := &protoproducer.ProducerConfig{}
	dec := yaml.NewDecoder(f)
	err := dec.Decode(config)
	return config, err
}

func main() {
	flag.Parse()

	if *Version {
		fmt.Println(AppVersion)
		os.Exit(0)
	}

	lvl, _ := log.ParseLevel(*LogLevel)
	log.SetLevel(lvl)

	switch *LogFmt {
	case "json":
		log.SetFormatter(&log.JSONFormatter{})
	}

	formatter, err := format.FindFormat(*Format)
	if err != nil {
		log.Fatal(err)
	}

	transporter, err := transport.FindTransport(*Transport)
	if err != nil {
		log.Fatal(err)
	}

	var flowProducer producer.ProducerInterface
	// instanciate a producer
	// unlike transport and format, the producer requires extensive configurations and can be chained
	if *Produce == "sample" {
		var cfgProducer *protoproducer.ProducerConfig
		if *MappingFile != "" {
			f, err := os.Open(*MappingFile)
			if err != nil {
				log.Fatal(err)
			}
			cfgProducer, err = LoadMapping(f)
			f.Close()
			if err != nil {
				log.Fatal(err)
			}
		}

		flowProducer, err = protoproducer.CreateProtoProducer(cfgProducer, protoproducer.CreateSamplingSystem())
		if err != nil {
			log.Fatal(err)
		}
	} else if *Produce == "raw" {
		flowProducer = &rawproducer.RawProducer{}
	} else {
		log.Fatalf("producer %s does not exist", *Produce)
	}

	// wrap producer with Prometheus metrics
	flowProducer = metrics.WrapPromProducer(flowProducer)

	log.Info("Starting GoFlow2")
	go httpServer()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	var receivers []*utils.UDPReceiver
	var pipes []utils.FlowPipe

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

		cfgPipe := &utils.PipeConfig{
			Format:           formatter,
			Transport:        transporter,
			Producer:         flowProducer,
			NetFlowTemplater: metrics.NewDefaultPromTemplateSystem, // wrap template system to get Prometheus info
		}

		var decodeFunc utils.DecoderFunc
		var p utils.FlowPipe
		if listenAddrUrl.Scheme == "sflow" {
			p = utils.NewSFlowPipe(cfgPipe)
		} else if listenAddrUrl.Scheme == "netflow" {
			p = utils.NewNetFlowPipe(cfgPipe)
		} else {
			l.Errorf("scheme %s does not exist", listenAddrUrl.Scheme)
			return
		}
		decodeFunc = metrics.PromDecoderWrapper(p.DecodeFlow, listenAddrUrl.Scheme)
		pipes = append(pipes, p)

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
						l := l.WithError(err)
						if errors.Is(err, netflow.ErrorTemplateNotFound) {
							l.Warn("template error")
						} else if errors.Is(err, net.ErrClosed) {
							l.Info("closed")
						} else {
							l.Error("error")
						}

					}
				}
			}()
			receivers = append(receivers, recv)
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		var transportErr <-chan error
		// specifically for Kafka errors
		if transportErrorFct, ok := transporter.TransportDriver.(interface {
			Errors() <-chan error
		}); ok {
			transportErr = transportErrorFct.Errors()
		}

		for {
			select {
			case <-q:
				return
			case err := <-transportErr:
				l := log.WithError(err)
				l.Error("transport error")
			}
		}
	}()

	<-c

	// stops receivers first, udp sockets will be down
	for _, recv := range receivers {
		recv.Stop()
	}
	// then stop pipe
	for _, pipe := range pipes {
		pipe.Close()
	}
	// close producer
	flowProducer.Close()
	// final close transporter (eg: flushes message to Kafka)
	transporter.Close()
	close(q) // close errors
	wg.Wait()

}
