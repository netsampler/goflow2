package utils

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"gopkg.in/yaml.v2"

	flowmessage "github.com/netsampler/goflow2/pb"
	"github.com/netsampler/goflow2/producer"
)

type ProducerConfig *producer.ProducerConfig

func LoadMapping(f io.Reader) (ProducerConfig, error) {
	config := &producer.ProducerConfig{}
	dec := yaml.NewDecoder(f)
	err := dec.Decode(config)
	return config, err
}

func GetServiceAddresses(srv string) (addrs []string, err error) {
	_, srvs, err := net.LookupSRV("", "", srv)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Service discovery: %v\n", err))
	}
	for _, srv := range srvs {
		addrs = append(addrs, net.JoinHostPort(srv.Target, strconv.Itoa(int(srv.Port))))
	}
	return addrs, nil
}

type BaseMessage struct {
	Src     net.IP
	Port    int
	Payload []byte

	SetTime  bool
	RecvTime time.Time
}

type Transport interface {
	Send([]*flowmessage.FlowMessage)
}

type Formatter interface {
	Format([]*flowmessage.FlowMessage)
}
