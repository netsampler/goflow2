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
	ready      chan bool
	q          chan bool
	wg         *sync.WaitGroup
	decodeFunc decoder.DecoderFunc
	dispatch   chan *udpPacket
	errCh      chan error

	decodersCnt int
	blocking    bool

	workers int
	sockets int

	Logger Logger
}

type UDPReceiverConfig struct {
	Workers   int
	Sockets   int
	Blocking  bool
	QueueSize int
}

func NewUDPReceiver(cfg *UDPReceiverConfig) *UDPReceiver {
	r := &UDPReceiver{
		wg:      &sync.WaitGroup{},
		sockets: 2,
		workers: 2,
		ready:   make(chan bool),
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

	r.init()

	return r
}

// Initialize channels that are related to a session
// Once the user calls Stop, they can restart the capture
func (r *UDPReceiver) init() error {
	r.errCh = make(chan error)
	r.q = make(chan bool)
	r.decodersCnt = 0
	select {
	case <-r.ready:
		return fmt.Errorf("Receiver is already stopped")
	default:
		close(r.ready)
	}
	return nil
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

// Start the processing routines
func (r *UDPReceiver) decoders(workers int) error {
	for i := 0; i < workers; i++ {
		r.wg.Add(1)
		r.decodersCnt += 1
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
						if err := r.decodeFunc(pkt); err != nil {
							select {
							case r.errCh <- err:
							default:
							}
						}
					}
					packetPool.Put(pkt)
				}
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
				select {
				case r.errCh <- err:
				default:
				}
			} // log error
		}()
		<-started
	}

	return nil
}

// Start UDP receivers and the processing routines
func (r *UDPReceiver) Start(addr string, port int) error {
	select {
	case <-r.ready:
		r.ready = make(chan bool)
	default:
		return fmt.Errorf("Receiver is already started")
	}
	if err := r.decoders(r.workers); err != nil {
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

	// close error chanel
	r.wg.Wait()
	close(r.errCh)
	return r.init() // recreates the closed channels
}
