package nats

import (
	"context"
	"flag"
	"github.com/netsampler/goflow2/transport"
	"github.com/netsampler/goflow2/utils"
	log "github.com/sirupsen/logrus"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

type NatsDriver struct {
	Logger             utils.Logger
	natsSubject        string
	natsURL            string
	natsCAPath         []string
	natsUser           string
	natsPass           string
	natsClientCertFile string
	natsClientKeyFile  string
	natsDialTimeout    time.Duration
	natsPingInterval   time.Duration
	natsMaxReconnect   int
	natsReconnectWait  time.Duration
	conn               *nats.Conn
	logErrors          bool
}

func (nd *NatsDriver) Prepare() error {

	flag.StringVar(&nd.natsSubject, "transport.nats.subject", "flow-messages", "Nats subject to publish on")
	flag.StringVar(&nd.natsURL, "transport.nats.url", nats.DefaultURL, "Nats URL to connect")
	flag.Var((*stringSliceFlag)(&nd.natsCAPath), "transport.nats.root-ca-path", "Root ca paths.  can be specified multiple times for multiple CA paths or separated by comma")
	flag.StringVar(&nd.natsClientCertFile, "transport.nats.client-cert", "", "Path to the nats client certificate if client auth is to be used.")
	flag.StringVar(&nd.natsClientKeyFile, "transport.nats.client-key", "", "Path to the nats client private key if client auth is to be used.")
	flag.StringVar(&nd.natsUser, "transport.nats.user", "", "Nats username")
	flag.StringVar(&nd.natsPass, "transport.nats.password", "", "Nats password")
	flag.DurationVar(&nd.natsDialTimeout, "transport.nats.dialtimeout", nats.GetDefaultOptions().Timeout, "Nats dial timeout for connecting to server(s).")
	flag.DurationVar(&nd.natsPingInterval, "transport.nats.ping_interval", nats.GetDefaultOptions().PingInterval, "Nats ping interval")
	flag.IntVar(&nd.natsMaxReconnect, "transport.nats.max_reconnect", nats.GetDefaultOptions().MaxReconnect, "Nats max reconnects before giving up")
	flag.DurationVar(&nd.natsReconnectWait, "transport.nats.reconnect_wait", nats.GetDefaultOptions().ReconnectWait, "Nats reconnect wait between attempts")
	flag.BoolVar(&nd.logErrors, "transport.nats.log.err", false, "log nats errors")
	return nil
}

func (nd *NatsDriver) Init(ctx context.Context) error {
	var options = []nats.Option{
		nats.Timeout(nd.natsDialTimeout),
		nats.PingInterval(nd.natsPingInterval),
		nats.MaxReconnects(nd.natsMaxReconnect),
		nats.ReconnectWait(nd.natsReconnectWait),
	}
	if len(nd.natsCAPath) > 0 {
		options = append(options, nats.RootCAs(nd.natsCAPath...))
	}
	if len(nd.natsClientCertFile) > 0 && len(nd.natsClientKeyFile) > 0 {
		options = append(options, nats.ClientCert(nd.natsClientCertFile, nd.natsClientKeyFile))
	}
	if len(nd.natsUser) > 0 || len(nd.natsPass) > 0 {
		options = append(options, nats.UserInfo(nd.natsUser, nd.natsPass))
	}
	var err error
	nd.conn, err = nats.Connect(nd.natsURL, options...)
	if err != nil {
		return err
	}
	if nd.logErrors {
		nd.conn.SetErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			log.Errorf("nats error: %s", err)
		})
	}
	return nil
}

func (nd *NatsDriver) Close(ctx context.Context) error {
	nd.conn.Close()
	return nil
}

func (nd *NatsDriver) Send(key, data []byte) error {
	return nd.conn.Publish(nd.natsSubject, data)
}

type stringSliceFlag []string

// String - implements flag.Value
func (ssf stringSliceFlag) String() string {
	return strings.Join(ssf, ",")
}

// Set - implements flag.Value.  If the flag is passed multiple times on the command line,
// each value appends to flags.  It also looks for comma separated values, and interprets
// those as individual items as well.
func (ssf *stringSliceFlag) Set(val string) error {
	vals := strings.Split(val, ",")
	*ssf = append(*ssf, vals...)
	return nil
}

func init() {
	transport.RegisterTransportDriver("nats", &NatsDriver{})
}
