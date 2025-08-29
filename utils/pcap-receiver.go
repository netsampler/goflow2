// +build pcap

//go:build pcap
package utils

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcap"
)

type PcapReceiver struct {
	ready    chan bool
	q        chan bool
	wg       *sync.WaitGroup
	dispatch chan *udpPacket
	errCh    chan error // linked to receiver, never closed

	decodersCnt int
	blocking    bool

	workers int
	sockets int

	cb ReceiverCallback
}

var _ Receiver = (*PcapReceiver)(nil)

func NewPcapReceiver(cfg *UDPReceiverConfig) (*PcapReceiver, error) {
	r := &PcapReceiver{
		wg:      &sync.WaitGroup{},
		sockets: 2,
		workers: 2,
		ready:   make(chan bool),
		errCh:   make(chan error),
	}

	return r, nil
}

func (r *PcapReceiver) Start(filename string, decodeFunc DecoderFunc) error {
	//select {
	//case <-r.ready:
	//	r.ready = make(chan bool)
	//default:
	//	return fmt.Errorf("receiver is already started")
	//}
	r.ready = make(chan bool)

	file, err := os.Open(filename)
	if err != nil {
		fmt.Printf("Error opening pcap: %v\n", err)
		return err
	}

	handle, err := pcap.OpenOfflineFile(file)
	if err != nil {
		fmt.Printf("Error using file as pcap: %v\n", err)
		return err
	}

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	for {
		packet, err := packetSource.NextPacket()
		if err != nil {
			fmt.Printf("Err nextpacket: %v", err)
			break
		}

		pkt := packet.NetworkLayer().NetworkFlow()

		var udp *layers.UDP
		var ok bool
		if udpLayer := packet.Layer(layers.LayerTypeUDP); udpLayer != nil {
			udp, ok = udpLayer.(*layers.UDP)
			if !ok {
				continue
			}
		}
		src, err := netip.ParseAddrPort(pkt.Src().String() + ":" + udp.SrcPort.String())
		if err != nil {
			return err
		}
		dstPort := strconv.Itoa(int(udp.DstPort))
		dst, err := netip.ParseAddrPort(pkt.Dst().String() + ":" + dstPort)
		if err != nil {
			return err
		}

		decodeFunc(&Message{
			Src:      src,
			Dst:      dst,
			Payload:  udp.Payload,
			Received: time.Now(),
		})
	}
	fmt.Println("end")

	return nil
}

func (r *PcapReceiver) Stop() error {
	return nil
}

func (r *PcapReceiver) Errors() <-chan error {
	return r.errCh
}
