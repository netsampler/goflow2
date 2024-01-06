package utils

import (
	"fmt"
	"net"
	"net/netip"
)

type MacAddress []byte // purely for the formatting purpose

func (s *MacAddress) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", net.HardwareAddr([]byte(*s)).String())), nil
}

type IPAddress []byte // purely for the formatting purpose

func (s IPAddress) MarshalJSON() ([]byte, error) {
	ip, _ := netip.AddrFromSlice([]byte(s))
	return []byte(fmt.Sprintf("\"%s\"", ip.String())), nil
}
