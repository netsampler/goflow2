//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package afpacket

import (
	"fmt"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"golang.org/x/sys/unix"
)

type packetRing struct {
	fd               int
	ring             []byte
	blockSize, count int
	timeout          time.Duration
	quiescedAt       time.Time
}

func openAFPacketRing(cfg config.CaptureConfig, index int) (*packetRing, error) {
	s, err := prepareAFPacketRing(cfg, index)
	if err != nil {
		return nil, err
	}
	if err := s.activate(index); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

// Prepare with protocol zero: allocate the replacement without admitting any
// packets until the old socket has stopped admissions and synchronized its hook.
func prepareAFPacketRing(cfg config.CaptureConfig, index int) (_ *packetRing, err error) {
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open AF_PACKET: %w", err)
	}
	s := &packetRing{fd: fd}
	defer func() {
		if err != nil {
			s.close()
		}
	}()
	if err = unix.SetsockoptInt(fd, unix.SOL_PACKET, unix.PACKET_VERSION, unix.TPACKET_V3); err != nil {
		return nil, fmt.Errorf("select TPACKET_V3: %w", err)
	}
	if cfg.Promiscuous == nil || *cfg.Promiscuous {
		if err = unix.SetsockoptPacketMreq(fd, unix.SOL_PACKET, unix.PACKET_ADD_MEMBERSHIP, &unix.PacketMreq{Ifindex: int32(index), Type: unix.PACKET_MR_PROMISC}); err != nil {
			return nil, fmt.Errorf("enable AF_PACKET promiscuous membership: %w", err)
		}
	}
	filter := socketFilter(cfg)
	if err = unix.SetsockoptSockFprog(fd, unix.SOL_SOCKET, unix.SO_ATTACH_FILTER, &unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}); err != nil {
		return nil, fmt.Errorf("set AF_PACKET capture filter: %w", err)
	}
	blockSize, count, timeout := cfg.AFPacket.BlockSize, cfg.AFPacket.NumBlocks, cfg.AFPacket.BlockTimeoutMS
	if blockSize == 0 {
		blockSize = 1 << 20
	}
	if count == 0 {
		count = 8
	}
	if timeout == 0 {
		timeout = 64
	}
	if blockSize < cfg.SnapLen+128 || blockSize%unix.Getpagesize() != 0 || blockSize%2048 != 0 || count < 1 || count > 1024 || timeout < 1 || uint64(blockSize)*uint64(count) > 512<<20 {
		return nil, fmt.Errorf("invalid AF_PACKET ring: blocks must fit snaplen, align to pages and 2048 bytes, and total at most 512 MiB")
	}
	s.blockSize, s.count, s.timeout = blockSize, count, time.Duration(timeout)*time.Millisecond
	req := unix.TpacketReq3{Block_size: uint32(blockSize), Block_nr: uint32(count), Frame_size: 2048, Frame_nr: uint32(blockSize / 2048 * count), Retire_blk_tov: uint32(timeout)}
	if err = unix.SetsockoptTpacketReq3(fd, unix.SOL_PACKET, unix.PACKET_RX_RING, &req); err != nil {
		return nil, fmt.Errorf("create AF_PACKET ring: %w", err)
	}
	if s.ring, err = unix.Mmap(fd, 0, blockSize*count, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED); err != nil {
		return nil, fmt.Errorf("map AF_PACKET ring: %w", err)
	}
	return s, nil
}

func (s *packetRing) activate(index int) error {
	if err := unix.Bind(s.fd, &unix.SockaddrLinklayer{Protocol: hostToNetwork(3), Ifindex: index}); err != nil {
		return fmt.Errorf("bind AF_PACKET: %w", err)
	}
	return nil
}

func (s *packetRing) setSampling(cfg config.CaptureConfig) error {
	filter := socketFilter(cfg)
	return unix.SetsockoptSockFprog(s.fd, unix.SOL_SOCKET, unix.SO_ATTACH_FILTER, &unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]})
}

func (s *packetRing) close() {
	if s.ring != nil {
		_ = unix.Munmap(s.ring)
		s.ring = nil
	}
	if s.fd >= 0 {
		_ = unix.Close(s.fd)
		s.fd = -1
	}
}

// Quiesce uses a rejecting filter, then a different protocol binding. Rebinding
// unregisters the ETH_P_ALL hook and synchronizes in-flight packet callbacks,
// letting the final PACKET_STATISTICS read account for every old-ring record.
func (s *packetRing) quiesce(index int) error {
	s.quiescedAt = time.Now()
	filter := []unix.SockFilter{{Code: unix.BPF_RET | unix.BPF_K, K: 0}}
	if err := unix.SetsockoptSockFprog(s.fd, unix.SOL_SOCKET, unix.SO_ATTACH_FILTER, &unix.SockFprog{Len: 1, Filter: &filter[0]}); err != nil {
		return err
	}
	return unix.Bind(s.fd, &unix.SockaddrLinklayer{Protocol: hostToNetwork(0x0800), Ifindex: index})
}
