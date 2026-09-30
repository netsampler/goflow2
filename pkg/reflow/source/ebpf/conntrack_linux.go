//go:build linux && !reflow_noebpf

package ebpf

import (
	"bufio"
	"encoding/binary"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

const conntrackRefreshInterval = 2 * time.Second

var defaultConntrackPaths = []string{
	"/proc/net/nf_conntrack",
	"/proc/net/ip_conntrack",
}

type conntrackTracker struct {
	path     string
	lastLoad time.Time
	entries  map[conntrackTuple]conntrackMetadata
}

type conntrackTuple struct {
	family  string
	proto   string
	src     netip.Addr
	dst     netip.Addr
	srcPort uint16
	dstPort uint16
}

type conntrackMetadata struct {
	direction string
	family    string
	proto     string
	state     string
	status    string
	original  conntrackTuple
	reply     conntrackTuple
	hasSNAT   bool
	natSrc    netip.Addr
	natSPort  uint16
	hasDNAT   bool
	natDst    netip.Addr
	natDPort  uint16
}

func newConntrackTracker(path string) *conntrackTracker {
	return &conntrackTracker{
		path:    path,
		entries: make(map[conntrackTuple]conntrackMetadata),
	}
}

func (t *conntrackTracker) Lookup(frame []byte) (conntrackMetadata, bool) {
	if t == nil {
		return conntrackMetadata{}, false
	}
	tuple, ok := packetConntrackTuple(frame)
	if !ok {
		return conntrackMetadata{}, false
	}
	if time.Since(t.lastLoad) >= conntrackRefreshInterval {
		t.refresh()
	}
	meta, ok := t.entries[tuple]
	return meta, ok
}

func (t *conntrackTracker) refresh() {
	t.lastLoad = time.Now()
	entries, ok := loadConntrackEntries(t.path)
	if ok {
		t.entries = entries
	}
}

func loadConntrackEntries(path string) (map[conntrackTuple]conntrackMetadata, bool) {
	paths := []string{path}
	if path == "" {
		paths = defaultConntrackPaths
	}
	for _, candidate := range paths {
		if candidate == "" {
			continue
		}
		entries, err := loadConntrackPath(candidate)
		if err == nil {
			return entries, true
		}
	}
	return nil, false
}

func loadConntrackPath(path string) (map[conntrackTuple]conntrackMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	entries := make(map[conntrackTuple]conntrackMetadata)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		registerConntrackLine(entries, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func registerConntrackLine(entries map[conntrackTuple]conntrackMetadata, line string) {
	meta, ok := parseConntrackLine(line)
	if !ok {
		return
	}
	orig := meta
	orig.direction = "original"
	reply := meta
	reply.direction = "reply"
	entries[meta.original] = orig
	entries[meta.reply] = reply
}

func parseConntrackLine(line string) (conntrackMetadata, bool) {
	tokens := strings.Fields(line)
	if len(tokens) < 4 {
		return conntrackMetadata{}, false
	}
	family := tokens[0]
	if family != "ipv4" && family != "ipv6" {
		return conntrackMetadata{}, false
	}
	protoIndex := -1
	proto := ""
	for i, token := range tokens {
		switch token {
		case "tcp", "udp":
			protoIndex = i
			proto = token
		}
		if protoIndex >= 0 {
			break
		}
	}
	if protoIndex < 0 {
		return conntrackMetadata{}, false
	}

	tuples := parseConntrackTuples(tokens[protoIndex+1:], family, proto)
	if len(tuples) < 2 {
		return conntrackMetadata{}, false
	}
	meta := conntrackMetadata{
		family:   family,
		proto:    proto,
		state:    parseConntrackState(tokens, protoIndex),
		status:   parseConntrackStatus(tokens),
		original: tuples[0],
		reply:    tuples[1],
	}
	if tuples[0].src != tuples[1].dst || tuples[0].srcPort != tuples[1].dstPort {
		meta.hasSNAT = true
		meta.natSrc = tuples[1].dst
		meta.natSPort = tuples[1].dstPort
	}
	if tuples[0].dst != tuples[1].src || tuples[0].dstPort != tuples[1].srcPort {
		meta.hasDNAT = true
		meta.natDst = tuples[1].src
		meta.natDPort = tuples[1].srcPort
	}
	return meta, true
}

func parseConntrackTuples(tokens []string, family, proto string) []conntrackTuple {
	var tuples []conntrackTuple
	var cur conntrackTuple
	cur.family = family
	cur.proto = proto
	haveSrc, haveDst, haveSPort, haveDPort := false, false, false, false
	reset := func() {
		cur = conntrackTuple{family: family, proto: proto}
		haveSrc, haveDst, haveSPort, haveDPort = false, false, false, false
	}
	for _, token := range tokens {
		key, val, ok := strings.Cut(token, "=")
		if !ok {
			continue
		}
		switch key {
		case "src":
			addr, err := netip.ParseAddr(val)
			if err == nil {
				cur.src = addr
				haveSrc = true
			}
		case "dst":
			addr, err := netip.ParseAddr(val)
			if err == nil {
				cur.dst = addr
				haveDst = true
			}
		case "sport":
			if port, ok := parseUint16(val); ok {
				cur.srcPort = port
				haveSPort = true
			}
		case "dport":
			if port, ok := parseUint16(val); ok {
				cur.dstPort = port
				haveDPort = true
			}
		}
		if haveSrc && haveDst && haveSPort && haveDPort {
			tuples = append(tuples, cur)
			reset()
		}
	}
	return tuples
}

func parseConntrackState(tokens []string, protoIndex int) string {
	for i := protoIndex + 1; i < len(tokens); i++ {
		token := tokens[i]
		if strings.Contains(token, "=") || strings.HasPrefix(token, "[") {
			continue
		}
		if _, err := strconv.Atoi(token); err == nil {
			continue
		}
		if strings.ToUpper(token) == token {
			return strings.ToLower(token)
		}
	}
	return ""
}

func parseConntrackStatus(tokens []string) string {
	var status []string
	for _, token := range tokens {
		if strings.HasPrefix(token, "[") && strings.HasSuffix(token, "]") {
			status = append(status, strings.ToLower(strings.Trim(token, "[]")))
		}
	}
	return strings.Join(status, ",")
}

func parseUint16(raw string) (uint16, bool) {
	val, err := strconv.ParseUint(raw, 10, 16)
	if err != nil {
		return 0, false
	}
	return uint16(val), true
}

func packetConntrackTuple(frame []byte) (conntrackTuple, bool) {
	if len(frame) < 14 {
		return conntrackTuple{}, false
	}
	offset := 14
	etherType := binary.BigEndian.Uint16(frame[12:14])
	for etherType == 0x8100 || etherType == 0x88a8 || etherType == 0x9100 {
		if len(frame) < offset+4 {
			return conntrackTuple{}, false
		}
		etherType = binary.BigEndian.Uint16(frame[offset+2 : offset+4])
		offset += 4
	}
	switch etherType {
	case 0x0800:
		return ipv4ConntrackTuple(frame[offset:])
	case 0x86dd:
		return ipv6ConntrackTuple(frame[offset:])
	default:
		return conntrackTuple{}, false
	}
}

func ipv4ConntrackTuple(packet []byte) (conntrackTuple, bool) {
	if len(packet) < 20 || packet[0]>>4 != 4 {
		return conntrackTuple{}, false
	}
	ihl := int(packet[0]&0x0f) * 4
	if ihl < 20 || len(packet) < ihl+4 {
		return conntrackTuple{}, false
	}
	proto := packet[9]
	src := netip.AddrFrom4([4]byte{packet[12], packet[13], packet[14], packet[15]})
	dst := netip.AddrFrom4([4]byte{packet[16], packet[17], packet[18], packet[19]})
	srcPort, dstPort, protoName, ok := transportTuple(packet[ihl:], proto)
	if !ok {
		return conntrackTuple{}, false
	}
	return conntrackTuple{
		family:  "ipv4",
		proto:   protoName,
		src:     src,
		dst:     dst,
		srcPort: srcPort,
		dstPort: dstPort,
	}, true
}

func ipv6ConntrackTuple(packet []byte) (conntrackTuple, bool) {
	if len(packet) < 40 || packet[0]>>4 != 6 {
		return conntrackTuple{}, false
	}
	nextHeader := packet[6]
	offset := 40
	for {
		switch nextHeader {
		case 0, 43, 60:
			if len(packet) < offset+2 {
				return conntrackTuple{}, false
			}
			nextHeader = packet[offset]
			offset += (int(packet[offset+1]) + 1) * 8
		case 44:
			if len(packet) < offset+8 {
				return conntrackTuple{}, false
			}
			fragmentOffset := binary.BigEndian.Uint16(packet[offset+2:offset+4]) & 0xfff8
			if fragmentOffset != 0 {
				return conntrackTuple{}, false
			}
			nextHeader = packet[offset]
			offset += 8
		default:
			goto transport
		}
		if len(packet) < offset+4 {
			return conntrackTuple{}, false
		}
	}

transport:
	if len(packet) < offset+4 {
		return conntrackTuple{}, false
	}
	src, ok := netip.AddrFromSlice(packet[8:24])
	if !ok {
		return conntrackTuple{}, false
	}
	dst, ok := netip.AddrFromSlice(packet[24:40])
	if !ok {
		return conntrackTuple{}, false
	}
	srcPort, dstPort, protoName, ok := transportTuple(packet[offset:], nextHeader)
	if !ok {
		return conntrackTuple{}, false
	}
	return conntrackTuple{
		family:  "ipv6",
		proto:   protoName,
		src:     src,
		dst:     dst,
		srcPort: srcPort,
		dstPort: dstPort,
	}, true
}

func transportTuple(packet []byte, proto uint8) (uint16, uint16, string, bool) {
	if len(packet) < 4 {
		return 0, 0, "", false
	}
	switch proto {
	case 6:
		return binary.BigEndian.Uint16(packet[0:2]), binary.BigEndian.Uint16(packet[2:4]), "tcp", true
	case 17:
		return binary.BigEndian.Uint16(packet[0:2]), binary.BigEndian.Uint16(packet[2:4]), "udp", true
	default:
		return 0, 0, "", false
	}
}

func applyConntrackFields(fields map[string]any, meta conntrackMetadata) {
	fields["conntrack_direction"] = meta.direction
	fields["conntrack_family"] = meta.family
	fields["conntrack_proto"] = meta.proto
	if meta.state != "" {
		fields["conntrack_state"] = meta.state
	}
	if meta.status != "" {
		fields["conntrack_status"] = meta.status
	}
	addTupleFields(fields, "conntrack_original", meta.original)
	// Keep reply tuple details out of exported fields; packetEvent attaches them
	// to Event.Internal so the processor can derive the NAT endpoint aliases.
	// addTupleFields(fields, "conntrack_reply", meta.reply)
}

func addTupleFields(fields map[string]any, prefix string, tuple conntrackTuple) {
	fields[prefix+"_src_addr"] = tuple.src.String()
	fields[prefix+"_dst_addr"] = tuple.dst.String()
	fields[prefix+"_src_port"] = uint32(tuple.srcPort)
	fields[prefix+"_dst_port"] = uint32(tuple.dstPort)
}
