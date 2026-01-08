package listen

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ListenerConfig defines a parsed listen address.
type ListenerConfig struct {
	Scheme     string
	Hostname   string
	Port       int
	NumSockets int
	NumWorkers int
	Blocking   bool
	QueueSize  int
}

// ParseListenAddresses parses a comma-separated list of listen URLs.
func ParseListenAddresses(spec string) ([]ListenerConfig, error) {
	var cfgs []ListenerConfig
	for _, listenAddress := range strings.Split(spec, ",") {
		listenAddrURL, err := url.Parse(listenAddress)
		if err != nil {
			return nil, fmt.Errorf("parse listen address %q: %w", listenAddress, err)
		}

		numSockets := 1
		if listenAddrURL.Query().Has("count") {
			numSocketsTmp, err := strconv.ParseUint(listenAddrURL.Query().Get("count"), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("error parsing count of sockets in URL: %w", err)
			}
			numSockets = int(numSocketsTmp)
		}
		if numSockets == 0 {
			numSockets = 1
		}

		numWorkers := 0
		if listenAddrURL.Query().Has("workers") {
			numWorkersTmp, err := strconv.ParseUint(listenAddrURL.Query().Get("workers"), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("error parsing workers in URL: %w", err)
			}
			numWorkers = int(numWorkersTmp)
		}
		if numWorkers == 0 {
			numWorkers = numSockets * 2
		}

		var isBlocking bool
		if listenAddrURL.Query().Has("blocking") {
			isBlocking, err = strconv.ParseBool(listenAddrURL.Query().Get("blocking"))
			if err != nil {
				return nil, fmt.Errorf("error parsing blocking in URL: %w", err)
			}
		}

		queueSize := 0
		if listenAddrURL.Query().Has("queue_size") {
			queueSizeTmp, err := strconv.ParseUint(listenAddrURL.Query().Get("queue_size"), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("error parsing queue_size in URL: %w", err)
			}
			queueSize = int(queueSizeTmp)
		} else if !isBlocking {
			queueSize = 1000000
		}

		port, err := strconv.ParseUint(listenAddrURL.Port(), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("port could not be converted to integer: %s: %w", listenAddrURL.Port(), err)
		}

		cfgs = append(cfgs, ListenerConfig{
			Scheme:     listenAddrURL.Scheme,
			Hostname:   listenAddrURL.Hostname(),
			Port:       int(port),
			NumSockets: numSockets,
			NumWorkers: numWorkers,
			Blocking:   isBlocking,
			QueueSize:  queueSize,
		})
	}

	return cfgs, nil
}
