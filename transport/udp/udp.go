package udp

import (
	"context"
	"flag"
	"github.com/netsampler/goflow2/transport"
	"net"
	"strconv"
	"sync"

	log "github.com/sirupsen/logrus"
)

type UdpDriver struct {
	udpDestination string
	udpPort        int
	udpSource      string
	packPb         bool
	mtu            int
	nbrSocks       int
	payloadBuf     []byte
	udpStreamers   []*net.UDPConn
	currentSock    int
	lock           *sync.RWMutex
	q              chan bool
}

func (d *UdpDriver) Prepare() error {
	flag.StringVar(&d.udpDestination, "transport.udp.dst", "", "Udp remote IP destination")
	flag.IntVar(&d.udpPort, "transport.udp.port", 10000, "Udp remote port")
	flag.StringVar(&d.udpSource, "transport.udp.src", "", "Udp local source IP address to use")
	flag.BoolVar(&d.packPb, "transport.udp.pack_pb", false, "Use this option to pack several pb message per UDP pkts. fixlen option and pb encoding are requiered")
	flag.IntVar(&d.mtu, "transport.udp.mtu", 1500, "Specify the mtu to avoid IP fragmentation")
	flag.IntVar(&d.nbrSocks, "transport.udp.num_socket", 1, "Use this option to have more entropy (one source port per socket) - useful for client that relies on REUSEPORT socket option")
	return nil
}

func (d *UdpDriver) Init(context.Context) error {
	d.q = make(chan bool)
	d.udpStreamers = make([]*net.UDPConn, d.nbrSocks)
	d.currentSock = 0
	// remove IP and UDP header
	d.mtu = d.mtu - 28

	// Just check if port is an Integer

	for i := 0; i < d.nbrSocks; i++ {
		localIP := ":" + strconv.Itoa(d.udpPort+i)
		if d.udpSource != "" {
			localIP = d.udpSource + ":" + strconv.Itoa(d.udpPort+i)
		}

		localAddr, err := net.ResolveUDPAddr("udp4", localIP)
		if err != nil {
			log.Errorf("Unable to resolve Local IP addr %s", localIP)
			return err
		}

		remoteAddr, err := net.ResolveUDPAddr("udp4", d.udpDestination+":"+strconv.Itoa(d.udpPort))
		if err != nil {
			log.Errorf("Unable to resolve Remote IP addr %s", d.udpDestination+":"+strconv.Itoa(d.udpPort))
			return err
		}

		d.udpStreamers[i], err = net.DialUDP("udp4", localAddr, remoteAddr)
		if err != nil {
			log.Errorf("Unable to create UDP Socket %v", err)
			return err
		}

	}

	return nil
}

func (d *UdpDriver) Send(key, data []byte) error {
	var err error
	d.lock.Lock()
	if d.packPb {
		if len(d.payloadBuf)+len(data) >= d.mtu {
			_, err = d.udpStreamers[d.currentSock].Write(d.payloadBuf)
			d.payloadBuf = nil
		}
		d.payloadBuf = append(d.payloadBuf, data...)
	} else {
		_, err = d.udpStreamers[d.currentSock].Write(data)
	}
	d.currentSock += 1
	if d.currentSock >= d.nbrSocks {
		d.currentSock = 0
	}
	d.lock.Unlock()

	return err
}

func (d *UdpDriver) Close(context.Context) error {
	for i := 0; i < d.nbrSocks; i++ {
		d.udpStreamers[i].Close()
	}
	close(d.q)
	return nil
}

func init() {
	d := &UdpDriver{
		lock: &sync.RWMutex{},
	}
	transport.RegisterTransportDriver("udp", d)
}
