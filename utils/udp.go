package utils

import (
	"fmt"
	"net"
	"sync"

	reuseport "github.com/libp2p/go-reuseport"

	decoder "github.com/netsampler/goflow2/decoders"
)

type udpPacket struct {
	src     *net.UDPAddr
	size    int
	payload []byte
}

var packetPool = sync.Pool{
	New: func() any {
		return &udpPacket{
			payload: make([]byte, 9000),
		}
	},
}

type UDPReceiver struct {
	q          chan bool
	wg         *sync.WaitGroup
	decodeFunc decoder.DecoderFunc
	dispatch   chan *udpPacket

	Logger Logger
}

func NewUDPReceiver() *UDPReceiver {
	return &UDPReceiver{
		q:  make(chan bool),
		wg: &sync.WaitGroup{},

		dispatch: make(chan *udpPacket, 1000000), // make it configurable
	}
}

func (r *UDPReceiver) routine(addr string, port int) error {
	pconn, err := reuseport.ListenPacket("udp", fmt.Sprintf("%s:%d", addr, port))
	if err != nil {
		return err
	}

	q := make(chan bool)
	// function to quit
	go func() {
		select {
		case <-q: // if routine has exited before
		case <-r.q: // upon general close
		}
		pconn.Close()
		return
	}()
	defer close(q)

	udpconn, ok := pconn.(*net.UDPConn)
	if !ok {
		return err
	}

	for {
		pkt := packetPool.Get().(*udpPacket)
		pkt.size, pkt.src, err = udpconn.ReadFromUDP(pkt.payload)
		if err != nil {
			// log
			continue
		}
		if pkt.size == 0 {
			// error
			continue
		}

		select {
		case r.dispatch <- pkt:
		default:
			packetPool.Put(pkt)
			// increase counter
		}
	}

}

func (r *UDPReceiver) StartDecoders(workers int) error {

	for i := 0; i < workers; i++ {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			for {

				select {
				case pkt := <-r.dispatch:
					err := r.decodeFunc(pkt)
					packetPool.Put(pkt)
					fmt.Println("debug", err)
				}
			}
		}()
	}

	return nil
}

func (r *UDPReceiver) Start(sockets int, addr string, port int) error {
	for i := 0; i < sockets; i++ {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.routine(addr, port) // log error
		}()
	}

	return nil
}

func (r *UDPReceiver) Stop() {
	select {
	case <-r.q:
	default:
		close(r.q)
	}
}
