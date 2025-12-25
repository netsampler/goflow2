package utils

import (
	"fmt"
	"net"
	"net/netip"
)

// MacAddress is a byte slice rendered as a MAC address in JSON.
type MacAddress []byte

// MarshalJSON formats the MAC address as a JSON string.
func (s *MacAddress) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", net.HardwareAddr([]byte(*s)).String())), nil
}

// IPAddress is a byte slice rendered as an IP address in JSON.
type IPAddress []byte

// MarshalJSON formats the IP address as a JSON string.
func (s IPAddress) MarshalJSON() ([]byte, error) {
	ip, _ := netip.AddrFromSlice([]byte(s))
	return []byte(fmt.Sprintf("\"%s\"", ip.String())), nil
}
