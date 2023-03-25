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

	decoders int
	blocking bool

	Logger Logger
}

type UDPReceiverConfig struct {
}

func NewUDPReceiver(cfg *UDPReceiverConfig) *UDPReceiver {
	r := &UDPReceiver{
		q:  make(chan bool),
		wg: &sync.WaitGroup{},
	}

	dispatchSize := 1000000
	if cfg != nil {

	}

	r.dispatch = make(chan *udpPacket, dispatchSize)

	return r
}

func (r *UDPReceiver) receive(addr string, port int, started chan bool) error {
	pconn, err := reuseport.ListenPacket("udp", fmt.Sprintf("%s:%d", addr, port))
	close(started)
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
			fmt.Println(err)
			return err
		}
		if pkt.size == 0 {
			// error
			continue
		}

		// add counters

		if r.blocking {
			select {
			case r.dispatch <- pkt:
			case <-r.q:
				return nil
			}
		} else {
			select {
			case r.dispatch <- pkt:
			case <-r.q:
				return nil
			default:
				packetPool.Put(pkt)
				// increase counter
			}
		}

	}

}

// Start the processing routines
func (r *UDPReceiver) Decoders(workers int) error {
	for i := 0; i < workers; i++ {
		r.wg.Add(1)
		r.decoders += 1
		go func() {
			defer r.wg.Done()
			for {

				select {
				case pkt := <-r.dispatch:
					fmt.Println(pkt)
					if pkt == nil {
						return
					}
					if r.decodeFunc != nil {
						err := r.decodeFunc(pkt)
						fmt.Println(err)
					}
					packetPool.Put(pkt)
				}
			}
		}()
	}

	return nil
}

// Starts the UDP receiving workers
func (r *UDPReceiver) Receivers(sockets int, addr string, port int) error {
	for i := 0; i < sockets; i++ {
		r.wg.Add(1)
		started := make(chan bool)
		go func() {
			defer r.wg.Done()
			r.receive(addr, port, started) // log error
		}()
		<-started
	}

	return nil
}

// Start UDP receivers and the processing routines
// Use this instead of starting Receivers and Decoders separately
// Creates the same amount of decoders as receivers
func (r *UDPReceiver) Start(workers int, addr string, port int) error {
	if err := r.Decoders(workers); err != nil {
		return err
	}
	if err := r.Receivers(workers, addr, port); err != nil {
		return err
	}
	return nil
}

func (r *UDPReceiver) Stop() {
	select {
	case <-r.q:
	default:
		close(r.q)
	}

	for i := 0; i < r.decoders; i++ {
		r.dispatch <- nil
	}

	// close error chanel
	r.wg.Wait()
	r.q = make(chan bool)
}
