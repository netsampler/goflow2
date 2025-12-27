package utils

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	reuseport "github.com/libp2p/go-reuseport"
)

// ReceiverCallback is notified when packets are dropped.
type ReceiverCallback interface {
	Dropped(msg Message)
}

// DecoderFunc decodes a received UDP message.
type DecoderFunc func(msg interface{}) error

type udpPacket struct {
	src      *net.UDPAddr
	dst      *net.UDPAddr
	size     int
	payload  []byte
	received time.Time
}

// Message carries a received UDP payload and metadata.
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

// UDPReceiver receives UDP packets and dispatches them to decoders.
type UDPReceiver struct {
	ready    chan bool
	q        chan bool
	recvWg   *sync.WaitGroup
	decodeWg *sync.WaitGroup
	dispatch chan *udpPacket
	errCh    chan error // linked to receiver, never closed

	blocking     bool
	dispatchSize int

	workers int
	sockets int

	cb ReceiverCallback
}

// UDPReceiverConfig configures UDP receiver workers and sockets.
type UDPReceiverConfig struct {
	Workers   int
	Sockets   int
	Blocking  bool
	QueueSize int

	ReceiverCallback ReceiverCallback
}

// NewUDPReceiver creates a UDP receiver with the provided configuration.
func NewUDPReceiver(cfg *UDPReceiverConfig) (*UDPReceiver, error) {
	r := &UDPReceiver{
		recvWg:   &sync.WaitGroup{},
		decodeWg: &sync.WaitGroup{},
		sockets:  2,
		workers:  2,
		ready:    make(chan bool),
		errCh:    make(chan error),
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
		r.cb = cfg.ReceiverCallback
	}
	r.dispatchSize = dispatchSize

	err := r.init()

	return r, err
}

// Initialize channels that are related to a session
// Once the user calls Stop, they can restart the capture
func (r *UDPReceiver) init() error {
	r.q = make(chan bool)
	if r.dispatchSize == 0 {
		r.dispatch = make(chan *udpPacket) // synchronous mode
	} else {
		r.dispatch = make(chan *udpPacket, r.dispatchSize)
	}
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

// Errors returns a channel of receiver errors.
func (r *UDPReceiver) Errors() <-chan error {
	return r.errCh
}

func (r *UDPReceiver) receive(addr string, port int, started chan bool) error {
	if strings.ContainsRune(addr, ':') && !strings.ContainsRune(addr, '[') {
		addr = "[" + addr + "]"
	}

	pconn, err := reuseport.ListenPacket("udp", fmt.Sprintf("%s:%d", addr, port))
	if err != nil {
		return err
	}
	close(started) // indicates receiver is setup

	q := make(chan bool)
	// function to quit
	go func() {
		select {
		case <-q: // if routine has exited before
		case <-r.q: // upon general close
		}
		if err := pconn.Close(); err != nil {
			r.logError(err)
		}
	}()
	defer close(q)

	udpconn, ok := pconn.(*net.UDPConn)
	if !ok {
		return fmt.Errorf("not a udp connection")
	}

	return r.receiveRoutine(udpconn)
}

func (r *UDPReceiver) receiveRoutine(udpconn *net.UDPConn) (err error) {
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
				if r.cb != nil {
					r.cb.Dropped(Message{
						Src:      pkt.src.AddrPort(),
						Dst:      pkt.dst.AddrPort(),
						Payload:  pkt.payload[0:pkt.size],
						Received: pkt.received,
					})
				}
				packetPool.Put(pkt)
				// increase counter
			}
		}

	}

}

// ReceiverError wraps errors from UDP receive routines.
type ReceiverError struct {
	Err error
}

func (e *ReceiverError) Error() string {
	return "receiver: " + e.Err.Error()
}

func (e *ReceiverError) Unwrap() error {
	return e.Err
}

// Start the processing routines.
func (r *UDPReceiver) decoders(workers int, decodeFunc DecoderFunc) error {
	for i := 0; i < workers; i++ {
		r.decodeWg.Add(1)
		go func() {
			defer r.decodeWg.Done()
			for pkt := range r.dispatch {
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

// receivers starts the UDP socket routines.
func (r *UDPReceiver) receivers(sockets int, addr string, port int) (rErr error) {
	for i := 0; i < sockets; i++ {
		if rErr != nil { // do not instanciate the rest of the receivers
			break
		}

		r.recvWg.Add(1)
		started := make(chan bool) // indicates receiver setup is complete
		go func() {
			defer r.recvWg.Done()
			if err := r.receive(addr, port, started); err != nil {
				err = &ReceiverError{err}

				select {
				case <-started:
				default: // in case the receiver is not started yet
					rErr = err
					close(started)
					return
				}

				r.logError(err)
			}
		}()
		<-started
	}

	return rErr
}

// Start runs UDP receivers and processing routines.
func (r *UDPReceiver) Start(addr string, port int, decodeFunc DecoderFunc) error {
	select {
	case <-r.ready:
		r.ready = make(chan bool)
	default:
		return fmt.Errorf("receiver is already started")
	}

	if err := r.decoders(r.workers, decodeFunc); err != nil {
		if stopErr := r.Stop(); stopErr != nil {
			return stopErr
		}
		return err
	}
	if err := r.receivers(r.sockets, addr, port); err != nil {
		if stopErr := r.Stop(); stopErr != nil {
			return stopErr
		}
		return err
	}
	return nil
}

// Stop stops the receiver and worker routines.
func (r *UDPReceiver) Stop() error {
	select {
	case <-r.ready:
		return fmt.Errorf("receiver is already stopped")
	default:
	}

	select {
	case <-r.q:
	default:
		close(r.q)
	}

	r.recvWg.Wait()
	close(r.dispatch)
	r.decodeWg.Wait()

	return r.init() // recreates the closed channels
}
