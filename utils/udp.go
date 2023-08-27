package utils

import (
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	reuseport "github.com/libp2p/go-reuseport"
)

// Callback used to decode a UDP message
type DecoderFunc func(msg interface{}) error

type udpPacket struct {
	src      *net.UDPAddr
	dst      *net.UDPAddr
	size     int
	payload  []byte
	received time.Time
}

type Message struct {
	Src      netip.AddrPort
	Dst      netip.AddrPort
	Payload  []byte
	Received time.Time
}

var packetPool = sync.Pool{
	New: func() any {
		return &udpPacket{
			payload: make([]byte, 9000),
		}
	},
}

type UDPReceiver struct {
	ready    chan bool
	q        chan bool
	wg       *sync.WaitGroup
	dispatch chan *udpPacket
	errCh    chan error // linked to receiver, never closed

	decodersCnt int
	blocking    bool

	workers int
	sockets int
}

type UDPReceiverConfig struct {
	Workers   int
	Sockets   int
	Blocking  bool
	QueueSize int
}

func NewUDPReceiver(cfg *UDPReceiverConfig) (*UDPReceiver, error) {
	r := &UDPReceiver{
		wg:      &sync.WaitGroup{},
		sockets: 2,
		workers: 2,
		ready:   make(chan bool),
		errCh:   make(chan error),
	}

	dispatchSize := 1000000
	if cfg != nil {
		if cfg.Sockets <= 0 {
			cfg.Sockets = 1
		}

		if cfg.Workers <= 0 {
			cfg.Workers = cfg.Sockets
		}

		r.sockets = cfg.Sockets
		r.workers = cfg.Workers
		dispatchSize = cfg.QueueSize
		r.blocking = cfg.Blocking
	}

	if dispatchSize == 0 {
		r.dispatch = make(chan *udpPacket) // synchronous mode
	} else {
		r.dispatch = make(chan *udpPacket, dispatchSize)
	}

	err := r.init()

	return r, err
}

// Initialize channels that are related to a session
// Once the user calls Stop, they can restart the capture
func (r *UDPReceiver) init() error {

	r.q = make(chan bool)
	r.decodersCnt = 0
	select {
	case <-r.ready:
		return fmt.Errorf("receiver is already stopped")
	default:
		close(r.ready)
	}
	return nil
}

func (r *UDPReceiver) logError(err error) {
	select {
	case r.errCh <- err:
	default:
	}
}

func (r *UDPReceiver) Errors() <-chan error {
	return r.errCh
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
	}()
	defer close(q)

	udpconn, ok := pconn.(*net.UDPConn)
	if !ok {
		return err
	}
	localAddr, _ := udpconn.LocalAddr().(*net.UDPAddr)

	for {
		pkt := packetPool.Get().(*udpPacket)
		pkt.size, pkt.src, err = udpconn.ReadFromUDP(pkt.payload)
		if err != nil {
			packetPool.Put(pkt)
			return err
		}
		pkt.dst = localAddr
		pkt.received = time.Now().UTC()
		if pkt.size == 0 {
			// error
			continue
		}

		if r.blocking {
			// does not drop
			// if combined with synchronous mode
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

type ReceiverError struct {
	Err error
}

func (e *ReceiverError) Error() string {
	return "receiver: " + e.Err.Error()
}

func (e *ReceiverError) Unwrap() error {
	return e.Err
}

// Start the processing routines
func (r *UDPReceiver) decoders(workers int, decodeFunc DecoderFunc) error {
	for i := 0; i < workers; i++ {
		r.wg.Add(1)
		r.decodersCnt += 1
		go func() {
			defer r.wg.Done()
			for pkt := range r.dispatch {

				if pkt == nil {
					return
				}
				if decodeFunc != nil {
					msg := Message{
						Src:      pkt.src.AddrPort(),
						Dst:      pkt.dst.AddrPort(),
						Payload:  pkt.payload[0:pkt.size],
						Received: pkt.received,
					}

					if err := decodeFunc(&msg); err != nil {
						r.logError(&ReceiverError{err})
					}
				}
				packetPool.Put(pkt)

			}
		}()
	}

	return nil
}

// Starts the UDP receiving workers
func (r *UDPReceiver) receivers(sockets int, addr string, port int) error {
	for i := 0; i < sockets; i++ {
		r.wg.Add(1)
		started := make(chan bool)
		go func() {
			defer r.wg.Done()
			if err := r.receive(addr, port, started); err != nil {
				r.logError(&ReceiverError{err})
			}
		}()
		<-started
	}

	return nil
}

// Start UDP receivers and the processing routines
func (r *UDPReceiver) Start(addr string, port int, decodeFunc DecoderFunc) error {
	select {
	case <-r.ready:
		r.ready = make(chan bool)
	default:
		return fmt.Errorf("receiver is already started")
	}

	if err := r.decoders(r.workers, decodeFunc); err != nil {
		return err
	}
	if err := r.receivers(r.workers, addr, port); err != nil {
		return err
	}
	return nil
}

// Stops the routines
func (r *UDPReceiver) Stop() error {
	select {
	case <-r.q:
	default:
		close(r.q)
	}

	for i := 0; i < r.decodersCnt; i++ {
		r.dispatch <- nil
	}

	r.wg.Wait()

	return r.init() // recreates the closed channels
}
