package main

import (
	"context"
	"flag"
	"log"
	"math/rand"
	"os"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
)

func main() {
	iface := flag.String("iface", "", "Interface to capture from")
	snaplen := flag.Int("snaplen", 1600, "Snapshot length")
	promisc := flag.Bool("promisc", true, "Enable promiscuous mode")
	readTimeout := flag.Duration("read-timeout", time.Second, "Read timeout")
	filter := flag.String("filter", "", "BPF filter")
	sampleRate := flag.Uint("sample-rate", 1, "Sample 1/N packets (1 = keep all)")
	sampleMode := flag.String("sample-mode", "count", "Sampling mode: count or random")
	sampleSeed := flag.Int64("sample-seed", 0, "Random sampler seed (0 uses current time)")
	flag.Parse()

	if *iface == "" {
		log.Fatal("iface is required")
	}

	handle, err := pcap.OpenLive(*iface, int32(*snaplen), *promisc, *readTimeout)
	if err != nil {
		log.Fatalf("open capture: %v", err)
	}
	defer handle.Close()

	if *filter != "" {
		if err := handle.SetBPFFilter(*filter); err != nil {
			log.Fatalf("set BPF filter: %v", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sampler := newSampler(*sampleRate, *sampleMode, *sampleSeed)

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	packets := packetSource.Packets()

	log.Printf("listening on interface %s", *iface)
	for {
		select {
		case <-ctx.Done():
			log.Print("stopping packet listener")
			return
		case packet, ok := <-packets:
			if !ok {
				return
			}
			if sampler.allow() {
				fmt.Println("Here", packet)
				samplePacket(packet)
			}
		}
	}
}

func samplePacket(packet gopacket.Packet) {
	// TODO: convert packet into flow samples and send them.
	_ = packet
}

type sampler struct {
	every   uint64
	counter uint64
	mode    string
	random  *rand.Rand
}

func newSampler(rate uint, mode string, seed int64) *sampler {
	if rate < 1 {
		rate = 1
	}
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	return &sampler{
		every:  uint64(rate),
		mode:   mode,
		random: rand.New(rand.NewSource(seed)),
	}
}

func (s *sampler) allow() bool {
	if s.every <= 1 {
		return true
	}
	switch s.mode {
	case "random":
		return s.random.Int63n(int64(s.every)) == 0
	default:
		s.counter++
		return s.counter%s.every == 0
	}
}
