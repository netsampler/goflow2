package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	// decoders
	"github.com/netsampler/goflow2/v2/decoders/netflow"

	// various formatters
	"github.com/netsampler/goflow2/v2/format"
	_ "github.com/netsampler/goflow2/v2/format/binary"
	_ "github.com/netsampler/goflow2/v2/format/json"
	_ "github.com/netsampler/goflow2/v2/format/text"

	// various transports
	"github.com/netsampler/goflow2/v2/transport"
	_ "github.com/netsampler/goflow2/v2/transport/file"
	_ "github.com/netsampler/goflow2/v2/transport/kafka"

	// various producers
	"github.com/netsampler/goflow2/v2/producer"
	protoproducer "github.com/netsampler/goflow2/v2/producer/proto"
	rawproducer "github.com/netsampler/goflow2/v2/producer/raw"

	// core libraries
	"github.com/netsampler/goflow2/v2/metrics"
	"github.com/netsampler/goflow2/v2/utils"
	"github.com/netsampler/goflow2/v2/utils/debug"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"
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

	ErrCnt = flag.Int("err.cnt", 10, "Maximum errors per batch for muting")
	ErrInt = flag.Duration("err.int", time.Second*10, "Maximum errors interval for muting")

	Addr = flag.String("addr", ":8080", "HTTP server address")

	TemplatePath = flag.String("templates.path", "/templates", "NetFlow/IPFIX templates list")

	MappingFile = flag.String("mapping", "", "Configuration file for custom mappings")

	Version = flag.Bool("v", false, "Print version")
)

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

	var loglevel slog.Level
	if err := loglevel.UnmarshalText([]byte(*LogLevel)); err != nil {
		log.Fatal("error parsing log level")
	}

	lo := slog.HandlerOptions{
		Level: loglevel,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &lo))

	switch *LogFmt {
	case "json":
		logger = slog.New(slog.NewJSONHandler(os.Stderr, &lo))
	}

	slog.SetDefault(logger)

	formatter, err := format.FindFormat(*Format)
	if err != nil {
		slog.Error("error formatter", slog.String("error", err.Error()))
		os.Exit(1)
	}

	transporter, err := transport.FindTransport(*Transport)
	if err != nil {
		slog.Error("error transporter", slog.String("error", err.Error()))
		os.Exit(1)
	}

	var flowProducer producer.ProducerInterface
	// instanciate a producer
	// unlike transport and format, the producer requires extensive configurations and can be chained
	if *Produce == "sample" {
		var cfgProducer *protoproducer.ProducerConfig
		if *MappingFile != "" {
			f, err := os.Open(*MappingFile)
			if err != nil {
				slog.Error("error opening mapping", slog.String("error", err.Error()))
				os.Exit(1)
			}
			cfgProducer, err = LoadMapping(f)
			f.Close()
			if err != nil {
				slog.Error("error loading mapping", slog.String("error", err.Error()))
				os.Exit(1)
			}
		}

		cfgm, err := cfgProducer.Compile() // converts configuration into a format that can be used by a protobuf producer
		if err != nil {
			log.Fatal(err)
		}

		flowProducer, err = protoproducer.CreateProtoProducer(cfgm, protoproducer.CreateSamplingSystem)
		if err != nil {
			slog.Error("error producer", slog.String("error", err.Error()))
			os.Exit(1)
		}
	} else if *Produce == "raw" {
		flowProducer = &rawproducer.RawProducer{}
	} else {
		slog.Error("producer does not exist", slog.String("error", err.Error()), slog.String("producer", *Produce))
		os.Exit(1)
	}

	// intercept panic and generate an error
	flowProducer = debug.WrapPanicProducer(flowProducer)
	// wrap producer with Prometheus metrics
	flowProducer = metrics.WrapPromProducer(flowProducer)

	wg := &sync.WaitGroup{}

	var collecting bool
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/__health", func(wr http.ResponseWriter, r *http.Request) {
		if !collecting {
			wr.WriteHeader(http.StatusServiceUnavailable)
			if _, err := wr.Write([]byte("Not OK\n")); err != nil {
				slog.Error("error writing HTTP", slog.String("error", err.Error()))
			}
		} else {
			wr.WriteHeader(http.StatusOK)
			if _, err := wr.Write([]byte("OK\n")); err != nil {
				slog.Error("error writing HTTP", slog.String("error", err.Error()))

			}
		}
	})
	srv := http.Server{
		Addr:              *Addr,
		ReadHeaderTimeout: time.Second * 5,
	}
	if *Addr != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger := logger.With(slog.String("http", *Addr))
			err := srv.ListenAndServe()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("HTTP server error", slog.String("error", err.Error()))
				os.Exit(1)
			}
			logger.Info("closed HTTP server")
		}()
	}

	logger.Info("starting GoFlow2")

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	var receivers []*utils.UDPReceiver
	var pipes []utils.FlowPipe

	q := make(chan bool)
	for _, listenAddress := range strings.Split(*ListenAddresses, ",") {
		listenAddrUrl, err := url.Parse(listenAddress)
		if err != nil {
			logger.Error("error parsing address", slog.String("error", err.Error()))
			os.Exit(1)
		}
		numSockets := 1
		if listenAddrUrl.Query().Has("count") {
			if numSocketsTmp, err := strconv.ParseUint(listenAddrUrl.Query().Get("count"), 10, 64); err != nil {
				slog.Error("error parsing count of sockets in URL", slog.String("error", err.Error()))
				os.Exit(1)
			} else {
				numSockets = int(numSocketsTmp)
			}
		}
		if numSockets == 0 {
			numSockets = 1
		}

		var numWorkers int
		if listenAddrUrl.Query().Has("workers") {
			if numWorkersTmp, err := strconv.ParseUint(listenAddrUrl.Query().Get("workers"), 10, 64); err != nil {
				slog.Error("error parsing workers in URL", slog.String("error", err.Error()))
				os.Exit(1)
			} else {
				numWorkers = int(numWorkersTmp)
			}
		}
		if numWorkers == 0 {
			numWorkers = numSockets * 2
		}

		var isBlocking bool
		if listenAddrUrl.Query().Has("blocking") {
			if isBlocking, err = strconv.ParseBool(listenAddrUrl.Query().Get("blocking")); err != nil {
				slog.Error("error parsing blocking in URL", slog.String("error", err.Error()))
				os.Exit(1)
			}
		}

		var queueSize int
		if listenAddrUrl.Query().Has("queue_size") {
			if queueSizeTmp, err := strconv.ParseUint(listenAddrUrl.Query().Get("queue_size"), 10, 64); err != nil {
				slog.Error("error parsing queue_size in URL", slog.String("error", err.Error()))
				os.Exit(1)
			} else {
				queueSize = int(queueSizeTmp)
			}
		} else if !isBlocking {
			queueSize = 1000000
		}

		hostname := listenAddrUrl.Hostname()
		port, err := strconv.ParseUint(listenAddrUrl.Port(), 10, 64)
		if err != nil {
			slog.Error("port could not be converted to integer", slog.String("port", listenAddrUrl.Port()))
			os.Exit(1)
		}

		logAttr := []any{
			slog.String("scheme", listenAddrUrl.Scheme),
			slog.String("hostname", hostname),
			slog.Int64("port", int64(port)),
			slog.Int("count", numSockets),
			slog.Int64("workers", int64(numWorkers)),
			slog.Bool("blocking", isBlocking),
			slog.Int64("queue_size", int64(queueSize)),
		}
		logger := logger.With(logAttr...)
		logger.Info("starting collection")

		cfg := &utils.UDPReceiverConfig{
			Sockets:          numSockets,
			Workers:          numWorkers,
			QueueSize:        queueSize,
			Blocking:         isBlocking,
			ReceiverCallback: metrics.NewReceiverMetric(),
		}
		recv, err := utils.NewUDPReceiver(cfg)
		if err != nil {
			logger.Error("error creating UDP receiver", slog.String("error", err.Error()))
			os.Exit(1)
		}

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
		} else if listenAddrUrl.Scheme == "flow" {
			p = utils.NewFlowPipe(cfgPipe)
		} else {
			logger.Error("scheme does not exist", slog.String("error", listenAddrUrl.Scheme))
			os.Exit(1)
		}

		decodeFunc = p.DecodeFlow
		// intercept panic and generate error
		decodeFunc = debug.PanicDecoderWrapper(decodeFunc)
		// wrap decoder with Prometheus metrics
		decodeFunc = metrics.PromDecoderWrapper(decodeFunc, listenAddrUrl.Scheme)
		pipes = append(pipes, p)

		bm := utils.NewBatchMute(*ErrInt, *ErrCnt)

		// starts receivers
		// the function either returns an error
		if err := recv.Start(hostname, int(port), decodeFunc); err != nil {
			logger.Error("error starting", slog.String("error", listenAddrUrl.Scheme))
			os.Exit(1)
		} else {
			wg.Add(1)
			go func() {
				defer wg.Done()

				for {
					select {
					case <-q:
						return
					case err := <-recv.Errors():
						if errors.Is(err, net.ErrClosed) {
							logger.Info("closed receiver")
							continue
						} else if !errors.Is(err, netflow.ErrorTemplateNotFound) && !errors.Is(err, debug.PanicError) {
							logger.Error("error", slog.String("error", err.Error()))
							continue
						}

						muted, skipped := bm.Increment()
						if muted && skipped == 0 {
							logger.Warn("too many receiver messages, muting")
						} else if !muted && skipped > 0 {
							logger.Warn("skipped receiver messages", slog.Int("count", skipped))
						} else if !muted {
							attrs := []any{
								slog.String("error", err.Error()),
							}

							if errors.Is(err, netflow.ErrorTemplateNotFound) {
								logger.Warn("template error")
							} else if errors.Is(err, debug.PanicError) {
								var pErrMsg *debug.PanicErrorMessage
								if errors.As(err, &pErrMsg) {
									attrs = append(attrs,
										slog.Any("message", pErrMsg.Msg),
										slog.String("stacktrace", string(pErrMsg.Stacktrace)),
									)
								}
								logger.Error("intercepted panic", attrs...)
							}
						}

					}
				}
			}()
			receivers = append(receivers, recv)
		}
	}

	// special routine to handle kafka errors transmitted as a stream
	wg.Add(1)
	go func() {
		defer wg.Done()

		var transportErr <-chan error
		if transportErrorFct, ok := transporter.TransportDriver.(interface {
			Errors() <-chan error
		}); ok {
			transportErr = transportErrorFct.Errors()
		}

		bm := utils.NewBatchMute(*ErrInt, *ErrCnt)

		for {
			select {
			case <-q:
				return
			case err := <-transportErr:
				if err == nil {
					return
				}
				muted, skipped := bm.Increment()
				if muted && skipped == 0 {
					logger.Warn("too many transport errors, muting")
				} else if !muted && skipped > 0 {
					logger.Warn("skipped transport errors", slog.Int("count", skipped))
				} else if !muted {
					logger.Error("transport error", slog.String("error", err.Error()))
				}

			}
		}
	}()

	collecting = true

	<-c

	collecting = false

	// stops receivers first, udp sockets will be down
	for _, recv := range receivers {
		if err := recv.Stop(); err != nil {
			logger.Error("error stopping receiver", slog.String("error", err.Error()))
		}
	}
	// then stop pipe
	for _, pipe := range pipes {
		pipe.Close()
	}
	// close producer
	flowProducer.Close()
	// close transporter (eg: flushes message to Kafka)
	transporter.Close()
	logger.Info("transporter closed")
	// close http server (prometheus + health check)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("error shutting-down HTTP server", slog.String("error", err.Error()))
	}
	cancel()
	close(q) // close errors
	wg.Wait()

}
