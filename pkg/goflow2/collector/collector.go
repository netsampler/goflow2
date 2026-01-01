package collector

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/format"
	"github.com/netsampler/goflow2/v3/metrics"
	"github.com/netsampler/goflow2/v3/pkg/goflow2/listen"
	"github.com/netsampler/goflow2/v3/producer"
	"github.com/netsampler/goflow2/v3/transport"
	"github.com/netsampler/goflow2/v3/utils"
	"github.com/netsampler/goflow2/v3/utils/debug"
	"github.com/netsampler/goflow2/v3/utils/templates"
)

// Config configures a Collector.
type Config struct {
	Listeners []listen.ListenerConfig
	Formatter format.FormatInterface
	Transport *transport.Transport
	Producer  producer.ProducerInterface
	ErrCnt    int
	ErrInt    time.Duration
	Logger    *slog.Logger

	TemplatesTTL            time.Duration
	TemplatesSweepInterval  time.Duration
	TemplatesExtendOnAccess bool
	TemplatesJSONPath       string
	TemplatesJSONInterval   time.Duration
}

// Collector manages receivers and flow pipes.
type Collector struct {
	listeners []listen.ListenerConfig
	formatter format.FormatInterface
	transport *transport.Transport
	producer  producer.ProducerInterface
	errCnt    int
	errInt    time.Duration
	logger    *slog.Logger

	receivers               []*utils.UDPReceiver
	pipes                   []utils.FlowPipe
	netflowTemplate         *utils.NetFlowPipe
	netFlowRegistry         templates.Registry
	jsonRegistry            *templates.JSONRegistry
	templatesTTL            time.Duration
	templatesSweepInterval  time.Duration
	templatesExtendOnAccess bool
	templatesJSONPath       string
	templatesJSONInterval   time.Duration
	stopCh                  chan struct{}
	wg                      sync.WaitGroup
}

// New creates a Collector from config.
func New(cfg Config) (*Collector, error) {
	if cfg.Logger == nil {
		return nil, errors.New("logger is required")
	}
	return &Collector{
		listeners:               cfg.Listeners,
		formatter:               cfg.Formatter,
		transport:               cfg.Transport,
		producer:                cfg.Producer,
		errCnt:                  cfg.ErrCnt,
		errInt:                  cfg.ErrInt,
		logger:                  cfg.Logger,
		netFlowRegistry:         nil,
		jsonRegistry:            nil,
		templatesTTL:            cfg.TemplatesTTL,
		templatesSweepInterval:  cfg.TemplatesSweepInterval,
		templatesExtendOnAccess: cfg.TemplatesExtendOnAccess,
		templatesJSONPath:       cfg.TemplatesJSONPath,
		templatesJSONInterval:   cfg.TemplatesJSONInterval,
	}, nil
}

// Start launches receivers and error handlers.
func (c *Collector) Start() error {
	c.stopCh = make(chan struct{})

	netFlowRegistry := templates.Registry(templates.NewInMemoryRegistry(nil))
	var jsonRegistry *templates.JSONRegistry
	if c.templatesJSONPath != "" {
		jsonRegistry = templates.NewJSONRegistry(
			c.templatesJSONPath,
			netFlowRegistry,
			templates.WithJSONFlushInterval(c.templatesJSONInterval),
		)
		netFlowRegistry = jsonRegistry
	}
	netFlowRegistry = metrics.NewPromTemplateRegistry(netFlowRegistry)
	expiring := templates.NewExpiringRegistry(
		netFlowRegistry,
		c.templatesTTL,
		templates.WithExtendOnAccess(c.templatesExtendOnAccess),
		templates.WithSweepInterval(c.templatesSweepInterval),
	)
	netFlowRegistry = expiring
	if c.templatesJSONPath != "" {
		if err := templates.PreloadJSONTemplates(c.templatesJSONPath, netFlowRegistry); err != nil {
			c.logger.Warn("error preloading templates JSON", slog.String("error", err.Error()))
		}
	}
	netFlowRegistry.Start()
	c.netFlowRegistry = netFlowRegistry
	c.jsonRegistry = jsonRegistry

	if jsonRegistry != nil {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()

			jsonErr := jsonRegistry.Errors()
			for {
				select {
				case <-c.stopCh:
					return
				case err, ok := <-jsonErr:
					if !ok {
						return
					}
					if err == nil {
						continue
					}
					c.logger.Error("template persistence error", slog.String("error", err.Error()))
				}
			}
		}()
	}

	for _, listenCfg := range c.listeners {
		logAttr := []any{
			slog.String("scheme", listenCfg.Scheme),
			slog.String("hostname", listenCfg.Hostname),
			slog.Int("port", listenCfg.Port),
			slog.Int("count", listenCfg.NumSockets),
			slog.Int("workers", listenCfg.NumWorkers),
			slog.Bool("blocking", listenCfg.Blocking),
			slog.Int("queue_size", listenCfg.QueueSize),
		}
		logger := c.logger.With(logAttr...)
		logger.Info("starting collection")

		recvCfg := &utils.UDPReceiverConfig{
			Sockets:          listenCfg.NumSockets,
			Workers:          listenCfg.NumWorkers,
			QueueSize:        listenCfg.QueueSize,
			Blocking:         listenCfg.Blocking,
			ReceiverCallback: metrics.NewReceiverMetric(),
		}
		recv, err := utils.NewUDPReceiver(recvCfg)
		if err != nil {
			return err
		}

		pipeCfg := &utils.PipeConfig{
			Format:          c.formatter,
			Transport:       c.transport,
			Producer:        c.producer,
			NetFlowRegistry: netFlowRegistry,
		}

		var p utils.FlowPipe
		switch listenCfg.Scheme {
		case "sflow":
			p = utils.NewSFlowPipe(pipeCfg)
		case "netflow":
			p = utils.NewNetFlowPipe(pipeCfg)
		case "flow":
			p = utils.NewFlowPipe(pipeCfg)
		default:
			return fmt.Errorf("scheme does not exist: %s", listenCfg.Scheme)
		}

		if nfP, ok := p.(*utils.NetFlowPipe); ok {
			c.netflowTemplate = nfP
		}

		decodeFunc := p.DecodeFlow
		decodeFunc = debug.PanicDecoderWrapper(decodeFunc)
		decodeFunc = metrics.PromDecoderWrapper(decodeFunc, listenCfg.Scheme)
		c.pipes = append(c.pipes, p)

		bm := utils.NewBatchMute(c.errInt, c.errCnt)

		if err := recv.Start(listenCfg.Hostname, listenCfg.Port, decodeFunc); err != nil {
			return err
		}
		c.wg.Add(1)
		go func(recv *utils.UDPReceiver, logger *slog.Logger) {
			defer c.wg.Done()
			for {
				select {
				case <-c.stopCh:
					return
				case err := <-recv.Errors():
					if errors.Is(err, net.ErrClosed) {
						logger.Info("closed receiver")
						continue
					} else if !errors.Is(err, netflow.ErrorTemplateNotFound) && !errors.Is(err, debug.ErrPanic) {
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
						} else if errors.Is(err, debug.ErrPanic) {
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
		}(recv, logger)

		c.receivers = append(c.receivers, recv)
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		var transportErr <-chan error
		if transportErrorFct, ok := c.transport.TransportDriver.(interface {
			Errors() <-chan error
		}); ok {
			transportErr = transportErrorFct.Errors()
		}

		bm := utils.NewBatchMute(c.errInt, c.errCnt)

		for {
			select {
			case <-c.stopCh:
				return
			case err := <-transportErr:
				if err == nil {
					return
				}
				muted, skipped := bm.Increment()
				if muted && skipped == 0 {
					c.logger.Warn("too many transport errors, muting")
				} else if !muted && skipped > 0 {
					c.logger.Warn("skipped transport errors", slog.Int("count", skipped))
				} else if !muted {
					c.logger.Error("transport error", slog.String("error", err.Error()))
				}
			}
		}
	}()

	return nil
}

// Stop stops receivers and pipes, then waits for goroutines.
func (c *Collector) Stop() {
	if c.stopCh != nil {
		close(c.stopCh)
	}

	for _, recv := range c.receivers {
		if err := recv.Stop(); err != nil {
			c.logger.Error("error stopping receiver", slog.String("error", err.Error()))
		}
	}
	for _, pipe := range c.pipes {
		pipe.Close()
	}
	if c.netFlowRegistry != nil {
		c.netFlowRegistry.Close()
	}
	c.wg.Wait()
}

// NetFlowTemplates returns templates from the last NetFlow pipe.
func (c *Collector) NetFlowTemplates() map[string]map[string]interface{} {
	if c.netflowTemplate == nil {
		return nil
	}
	return c.netflowTemplate.GetTemplatesForAllSources()
}
