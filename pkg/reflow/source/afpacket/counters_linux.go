//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package afpacket

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type interfaceBytes struct{ rx, tx uint64 }

// These device counters bracket the handoff; reads are not atomic with each
// other or packet admission. Log their interval separately from socket pause.
func readInterfaceBytes(iface string) (interfaceBytes, error) {
	read := func(name string) (uint64, error) {
		raw, err := os.ReadFile(filepath.Join("/sys/class/net", iface, "statistics", name))
		if err != nil {
			return 0, err
		}
		return strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	}
	rx, err := read("rx_bytes")
	if err != nil {
		return interfaceBytes{}, err
	}
	tx, err := read("tx_bytes")
	if err != nil {
		return interfaceBytes{}, err
	}
	return interfaceBytes{rx: rx, tx: tx}, nil
}
