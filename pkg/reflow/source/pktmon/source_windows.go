//go:build windows && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package pktmon

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
	"golang.org/x/sys/windows"
)

// The API intentionally has no SDK header/import library. These declarations
// follow Microsoft's PacketMonitor streaming documentation and native Windows
// alignment on amd64/arm64.
type sourceSpec struct {
	Kind                               uint32
	Name                               [64]uint16
	Description                        [128]uint16
	ID, SecondaryID, ParentID, Present uint32
	Detail                             [2]uint64
}
type sourceList struct {
	Count uint32
	First *sourceSpec
}
type streamConfig struct {
	Context, EventCallback, DataCallback uintptr
	Multiplier, Truncation               uint16
}
type streamData struct {
	Data                                                                      unsafe.Pointer
	Size, MetadataOffset, PacketOffset, PacketLength, MissedWrite, MissedRead uint32
}
type identity struct {
	Name  string
	Index int
}
type driver struct {
	cfg                            config.SourceConfig
	done                           chan struct{}
	once                           sync.Once
	queue                          chan capture.Frame
	fatal                          chan error
	ports                          map[uint32]identity
	readLoss, writeLoss, queueLoss atomic.Uint64
}

var (
	registry      sync.Map
	nextContext   atomic.Uint64
	dataCallback  = windows.NewCallback(onPacket)
	eventCallback = windows.NewCallback(onEvent)
)

func New(cfg config.SourceConfig) (*capture.Source, error) {
	if err := config.NormalizeCapture(&cfg); err != nil {
		return nil, err
	}
	if cfg.Capture.Promiscuous != nil && *cfg.Capture.Promiscuous {
		return nil, fmt.Errorf("pktmon does not support promiscuous mode")
	}
	if cfg.Capture.SnapLen > 9000 {
		return nil, fmt.Errorf("pktmon snaplen must not exceed 9000 bytes")
	}
	n := cfg.Capture.Pktmon.QueueSize
	if n <= 0 {
		n = 1024
	}
	d := &driver{cfg: cfg, done: make(chan struct{}), queue: make(chan capture.Frame, n), fatal: make(chan error, 1), ports: make(map[uint32]identity)}
	return capture.New(cfg, d)
}
func (d *driver) Close() error { d.once.Do(func() { close(d.done) }); return nil }

func onEvent(context uintptr, _ unsafe.Pointer, kind uint32) uintptr {
	if value, ok := registry.Load(context); ok && kind == 2 {
		d := value.(*driver)
		select {
		case d.fatal <- fmt.Errorf("packet monitor streaming stopped with a fatal error"):
		default:
		}
	}
	return 0
}
func onPacket(context uintptr, packet *streamData) uintptr {
	value, ok := registry.Load(context)
	if !ok || packet == nil {
		return 0
	}
	d := value.(*driver)
	d.readLoss.Add(uint64(packet.MissedRead))
	d.writeLoss.Add(uint64(packet.MissedWrite))
	if packet.Size > 16*1024*1024 || packet.Data == nil || packet.MetadataOffset > packet.Size || packet.PacketOffset > packet.Size || packet.PacketLength > packet.Size-packet.PacketOffset {
		return 0
	}
	buf := unsafe.Slice((*byte)(packet.Data), int(packet.Size))
	frame := decodePacket(append([]byte(nil), buf[packet.PacketOffset:packet.PacketOffset+packet.PacketLength]...), buf[packet.MetadataOffset:])
	if component, ok := frame.Fields["pktmon_component_id"].(uint32); ok {
		if port, exists := d.ports[component]; exists {
			frame.Interface = port.Name
			frame.InterfaceIndex = port.Index
		}
	}
	if direction := d.cfg.Capture.Direction; direction != "both" && direction != "" && direction != frame.Direction {
		return 0
	}
	frame.Drops = d.readLoss.Load() + d.writeLoss.Load() + d.queueLoss.Load()
	if frame.Fields == nil {
		frame.Fields = make(map[string]any)
	}
	frame.Fields["pktmon_missed_reads"] = d.readLoss.Load()
	frame.Fields["pktmon_missed_writes"] = d.writeLoss.Load()
	frame.Fields["capture_queue_drops"] = d.queueLoss.Load()
	select {
	case <-d.done:
		return 0
	case d.queue <- frame:
	default:
		d.queueLoss.Add(1)
	}
	return 0
}

func hresult(proc *windows.LazyProc, args ...uintptr) error {
	code, _, _ := proc.Call(args...)
	if int32(code) < 0 {
		return fmt.Errorf("%s: HRESULT 0x%08x (Packet Monitor requires administrator privileges and the streaming API)", proc.Name, uint32(code))
	}
	return nil
}

func enumerate(proc *windows.LazyProc, handle uintptr, hidden bool) ([]*sourceSpec, []byte, error) {
	var needed uintptr
	show := uintptr(0)
	if hidden {
		show = 1
	}
	_, _, _ = proc.Call(handle, 1, show, 0, uintptr(unsafe.Pointer(&needed)), 0)
	if needed == 0 || needed > 64*1024*1024 {
		return nil, nil, fmt.Errorf("packet monitor returned invalid data-source buffer length %d", needed)
	}
	for attempt := 0; attempt < 3; attempt++ {
		buf := make([]byte, int(needed))
		size := needed
		err := hresult(proc, handle, 1, show, size, uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&buf[0])))
		if err != nil {
			if needed > size {
				continue
			}
			return nil, nil, err
		}
		list := (*sourceList)(unsafe.Pointer(&buf[0]))
		if uintptr(list.Count) > (size-unsafe.Offsetof(list.First))/unsafe.Sizeof(list.First) {
			return nil, nil, fmt.Errorf("invalid Packet Monitor data-source count")
		}
		return unsafe.Slice(&list.First, int(list.Count)), buf, nil
	}
	return nil, nil, fmt.Errorf("packet monitor data sources changed during enumeration")
}

func adapterMap() (map[string]identity, error) {
	size := uint32(16 * 1024)
	for attempt := 0; attempt < 3; attempt++ {
		buf := make([]byte, size)
		head := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, windows.GAA_FLAG_INCLUDE_ALL_INTERFACES, 0, head, &size)
		if err == windows.ERROR_BUFFER_OVERFLOW {
			continue
		}
		if err != nil {
			return nil, err
		}
		result := make(map[string]identity)
		for p := head; p != nil; p = p.Next {
			name := windows.UTF16PtrToString(p.FriendlyName)
			index := p.IfIndex
			if index == 0 {
				index = p.Ipv6IfIndex
			}
			id := identity{Name: name, Index: int(index)}
			result[strings.ToLower(windows.BytePtrToString(p.AdapterName))] = id
			result[strings.ToLower(name)] = id
		}
		return result, nil
	}
	return nil, fmt.Errorf("network adapters changed during enumeration")
}

func identify(spec *sourceSpec, adapters map[string]identity) identity {
	name := windows.UTF16ToString(spec.Name[:])
	if port, ok := adapters[strings.ToLower(name)]; ok {
		return port
	}
	if spec.Present&1 != 0 {
		guid := (*windows.GUID)(unsafe.Pointer(&spec.Detail[0]))
		if port, ok := adapters[strings.ToLower(guid.String())]; ok {
			return port
		}
	}
	return identity{Name: name}
}

func (d *driver) Run(ctx context.Context, emit func(capture.Frame) error) error {
	select {
	case <-ctx.Done():
		return nil
	case <-d.done:
		return nil
	default:
	}
	dll := windows.NewLazySystemDLL("Pktmonapi.dll")
	names := []string{"PacketMonitorInitialize", "PacketMonitorUninitialize", "PacketMonitorCreateLiveSession", "PacketMonitorCloseSessionHandle", "PacketMonitorSetSessionActive", "PacketMonitorEnumDataSources", "PacketMonitorAddSingleDataSourceToSession", "PacketMonitorCreateRealtimeStream", "PacketMonitorCloseRealtimeStream", "PacketMonitorAttachOutputToSession"}
	api := make(map[string]*windows.LazyProc, len(names))
	for _, name := range names {
		proc := dll.NewProc(name)
		if err := proc.Find(); err != nil {
			return fmt.Errorf("pktmon requires Windows Packet Monitor streaming API (%s): %w", name, err)
		}
		api[name] = proc
	}
	var handle, session, stream uintptr
	if err := hresult(api[names[0]], 0x10000, 0, uintptr(unsafe.Pointer(&handle))); err != nil {
		return err
	}
	defer func() { _, _, _ = api[names[1]].Call(handle) }()
	id := uintptr(nextContext.Add(1))
	name, _ := windows.UTF16PtrFromString(fmt.Sprintf("Reflow-%d-%d", os.Getpid(), id))
	if err := hresult(api[names[2]], handle, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&session))); err != nil {
		return err
	}
	defer func() { _, _, _ = api[names[3]].Call(session) }()
	adapters, err := adapterMap()
	if err != nil {
		return fmt.Errorf("map Windows interfaces: %w", err)
	}
	specs, storage, err := enumerate(api[names[5]], handle, false)
	if err != nil {
		return err
	}
	selected := 0
	for _, spec := range specs {
		port := identify(spec, adapters)
		if target := d.cfg.Capture.Interface; target != "any" && target != "all" && !strings.EqualFold(port.Name, target) && !strings.EqualFold(windows.UTF16ToString(spec.Name[:]), target) {
			continue
		}
		if err = hresult(api[names[6]], session, uintptr(unsafe.Pointer(spec))); err != nil {
			return err
		}
		selected++
	}
	runtime.KeepAlive(storage)
	if selected == 0 {
		return fmt.Errorf("no Packet Monitor network-interface data source matches %q", d.cfg.Capture.Interface)
	}
	hidden, storage, err := enumerate(api[names[5]], handle, true)
	if err != nil {
		return err
	}
	for _, spec := range hidden {
		port := identify(spec, adapters)
		d.ports[spec.ID] = port
		if spec.SecondaryID != 0 {
			d.ports[spec.SecondaryID] = port
		}
	}
	runtime.KeepAlive(storage)
	registry.Store(id, d)
	defer registry.Delete(id)
	conf := streamConfig{Context: id, EventCallback: eventCallback, DataCallback: dataCallback, Multiplier: 10, Truncation: uint16(d.cfg.Capture.SnapLen)}
	if err = hresult(api[names[7]], handle, uintptr(unsafe.Pointer(&conf)), uintptr(unsafe.Pointer(&stream))); err != nil {
		return err
	}
	defer func() { _, _, _ = api[names[8]].Call(stream) }()
	if err = hresult(api[names[9]], session, stream); err != nil {
		return err
	}
	if err = hresult(api[names[4]], session, 1); err != nil {
		return err
	}
	defer func() { _, _, _ = api[names[4]].Call(session, 0) }()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-d.done:
			return nil
		case err := <-d.fatal:
			return err
		case frame := <-d.queue:
			if err := emit(frame); err != nil {
				return err
			}
		}
	}
}
