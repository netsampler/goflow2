package producer

import (
	"fmt"
	"net/netip"
	"sync"
	"time"
)

// Interface of the messages
type ProducerMessage interface{}

type ProducerInterface interface {
	// Converts a message into a list of flow samples
	Produce(msg interface{}, args *ProduceArgs) ([]ProducerMessage, error)
	// Indicates to the producer the messages returned were processed
	Commit([]ProducerMessage)
	Close()
}

type ProduceArgs struct {
	Src            netip.AddrPort
	Dst            netip.AddrPort
	SamplerAddress netip.Addr
	TimeReceived   time.Time
}

var (
	producers = make(map[string]ProducerInterface)
	lock      = &sync.RWMutex{}
)

func RegisterProducer(name string, t ProducerInterface) {
	lock.Lock()
	producers[name] = t
	lock.Unlock()

	/*if err := t.Prepare(); err != nil {
		panic(err) // ?improve?
	}*/
}

func FindProducer(name string) (ProducerInterface, error) {
	lock.RLock()
	t, ok := producers[name]
	lock.RUnlock()
	if !ok {
		return nil, fmt.Errorf("Producer %s not found", name)
	}

	/*err := t.Init()
	return t, err*/
	return t, nil
}

func GetProducers() []string {
	lock.RLock()
	defer lock.RUnlock()
	t := make([]string, len(producers))
	var i int
	for k, _ := range producers {
		t[i] = k
		i++
	}
	return t
}
