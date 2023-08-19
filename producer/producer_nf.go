package producer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/netsampler/goflow2/decoders/netflow"
	"github.com/netsampler/goflow2/decoders/utils"
	flowmessage "github.com/netsampler/goflow2/pb"
)

type SamplingRateSystem interface {
	GetSamplingRate(version uint16, obsDomainId uint32) (uint32, error)
	AddSamplingRate(version uint16, obsDomainId uint32, samplingRate uint32)
}

type basicSamplingRateSystem struct {
	sampling     map[uint16]map[uint32]uint32
	samplinglock *sync.RWMutex
}

func CreateSamplingSystem() SamplingRateSystem {
	ts := &basicSamplingRateSystem{
		sampling:     make(map[uint16]map[uint32]uint32),
		samplinglock: &sync.RWMutex{},
	}
	return ts
}

func (s *basicSamplingRateSystem) AddSamplingRate(version uint16, obsDomainId uint32, samplingRate uint32) {
	s.samplinglock.Lock()
	defer s.samplinglock.Unlock()
	_, exists := s.sampling[version]
	if exists != true {
		s.sampling[version] = make(map[uint32]uint32)
	}
	s.sampling[version][obsDomainId] = samplingRate
}

func (s *basicSamplingRateSystem) GetSamplingRate(version uint16, obsDomainId uint32) (uint32, error) {
	s.samplinglock.RLock()
	defer s.samplinglock.RUnlock()
	samplingVersion, okver := s.sampling[version]
	if okver {
		samplingRate, okid := samplingVersion[obsDomainId]
		if okid {
			return samplingRate, nil
		}
		return 0, errors.New("") // TBC
	}
	return 0, errors.New("") // TBC
}

type SingleSamplingRateSystem struct {
	Sampling uint32
}

func (s *SingleSamplingRateSystem) AddSamplingRate(version uint16, obsDomainId uint32, samplingRate uint32) {
}

func (s *SingleSamplingRateSystem) GetSamplingRate(version uint16, obsDomainId uint32) (uint32, error) {
	return s.Sampling, nil
}

func NetFlowLookFor(dataFields []netflow.DataField, typeId uint16) (bool, interface{}) {
	for _, dataField := range dataFields {
		if dataField.Type == typeId {
			return true, dataField.Value
		}
	}
	return false, nil
}

func NetFlowPopulate(dataFields []netflow.DataField, typeId uint16, addr interface{}) bool {
	exists, value := NetFlowLookFor(dataFields, typeId)
	if exists && value != nil {
		valueBytes, ok := value.([]byte)
		valueReader := bytes.NewBuffer(valueBytes)
		if ok {
			switch addrt := addr.(type) {
			case *(net.IP):
				*addrt = valueBytes
			case *(time.Time):
				t := uint64(0)
				utils.BinaryRead(valueReader, binary.BigEndian, &t)
				t64 := int64(t / 1000)
				*addrt = time.Unix(t64, 0)
			default:
				utils.BinaryRead(valueReader, binary.BigEndian, addr)
			}
		}
	}
	return exists
}

func WriteUDecoded(o uint64, out interface{}) error {
	switch t := out.(type) {
	case *byte:
		*t = byte(o)
	case *uint16:
		*t = uint16(o)
	case *uint32:
		*t = uint32(o)
	case *uint64:
		*t = o
	default:
		return errors.New("The parameter is not a pointer to a byte/uint16/uint32/uint64 structure")
	}
	return nil
}

func WriteDecoded(o int64, out interface{}) error {
	switch t := out.(type) {
	case *int8:
		*t = int8(o)
	case *int16:
		*t = int16(o)
	case *int32:
		*t = int32(o)
	case *int64:
		*t = o
	default:
		return errors.New("The parameter is not a pointer to a int8/int16/int32/int64 structure")
	}
	return nil
}

func DecodeUNumber(b []byte, out interface{}) error {
	var o uint64
	l := len(b)
	switch l {
	case 1:
		o = uint64(b[0])
	case 2:
		o = uint64(binary.BigEndian.Uint16(b))
	case 4:
		o = uint64(binary.BigEndian.Uint32(b))
	case 8:
		o = binary.BigEndian.Uint64(b)
	default:
		if l < 8 {
			var iter uint
			for i := range b {
				o |= uint64(b[i]) << uint(8*(uint(l)-iter-1))
				iter++
			}
		} else {
			return errors.New(fmt.Sprintf("Non-regular number of bytes for a number: %v", l))
		}
	}
	return WriteUDecoded(o, out)
}

func DecodeUNumberLE(b []byte, out interface{}) error {
	var o uint64
	l := len(b)
	switch l {
	case 1:
		o = uint64(b[0])
	case 2:
		o = uint64(binary.LittleEndian.Uint16(b))
	case 4:
		o = uint64(binary.LittleEndian.Uint32(b))
	case 8:
		o = binary.LittleEndian.Uint64(b)
	default:
		if l < 8 {
			var iter uint
			for i := range b {
				o |= uint64(b[i]) << uint(8*(iter))
				iter++
			}
		} else {
			return errors.New(fmt.Sprintf("Non-regular number of bytes for a number: %v", l))
		}
	}
	return WriteUDecoded(o, out)
}

func DecodeNumber(b []byte, out interface{}) error {
	var o int64
	l := len(b)
	switch l {
	case 1:
		o = int64(int8(b[0]))
	case 2:
		o = int64(int16(binary.BigEndian.Uint16(b)))
	case 4:
		o = int64(int32(binary.BigEndian.Uint32(b)))
	case 8:
		o = int64(binary.BigEndian.Uint64(b))
	default:
		if l < 8 {
			var iter int
			for i := range b {
				o |= int64(b[i]) << int(8*(int(l)-iter-1))
				iter++
			}
		} else {
			return errors.New(fmt.Sprintf("Non-regular number of bytes for a number: %v", l))
		}
	}
	return WriteDecoded(o, out)
}

func DecodeNumberLE(b []byte, out interface{}) error {
	var o int64
	l := len(b)
	switch l {
	case 1:
		o = int64(int8(b[0]))
	case 2:
		o = int64(int16(binary.LittleEndian.Uint16(b)))
	case 4:
		o = int64(int32(binary.LittleEndian.Uint32(b)))
	case 8:
		o = int64(binary.LittleEndian.Uint64(b))
	default:
		if l < 8 {
			var iter int
			for i := range b {
				o |= int64(b[i]) << int(8*(iter))
				iter++
			}
		} else {
			return errors.New(fmt.Sprintf("Non-regular number of bytes for a number: %v", l))
		}
	}
	return WriteDecoded(o, out)
}

func allZeroes(v []byte) bool {
	for _, b := range v {
		if b != 0 {
			return false
		}
	}
	return true
}

func addrReplaceCheck(dstAddr *[]byte, v []byte, eType *uint32, ipv6 bool) {
	if (len(*dstAddr) == 0 && len(v) > 0) ||
		(len(*dstAddr) != 0 && len(v) > 0 && !allZeroes(v)) {
		*dstAddr = v

		if ipv6 {
			*eType = 0x86dd
		} else {
			*eType = 0x800
		}

	}
}

func ConvertNetFlowDataSet(version uint16, baseTime uint32, uptime uint32, record []netflow.DataField, mapperNetFlow *NetFlowMapper, mapperSFlow *SFlowMapper) *flowmessage.FlowMessage {
	flowMessage := &flowmessage.FlowMessage{}
	var time uint64

	if version == 9 {
		flowMessage.Type = flowmessage.FlowMessage_NETFLOW_V9
	} else if version == 10 {
		flowMessage.Type = flowmessage.FlowMessage_IPFIX
	}

	for i := range record {
		df := record[i]

		v, ok := df.Value.([]byte)
		if !ok {
			continue
		}

		MapCustomNetFlow(flowMessage, df, mapperNetFlow)

		if df.PenProvided {
			continue
		}

		switch df.Type {

		case netflow.IPFIX_FIELD_observationPointId:
			DecodeUNumber(v, &(flowMessage.ObservationPointId))

		// Statistics
		case netflow.NFV9_FIELD_IN_BYTES:
			DecodeUNumber(v, &(flowMessage.Bytes))
		case netflow.NFV9_FIELD_IN_PKTS:
			DecodeUNumber(v, &(flowMessage.Packets))
		case netflow.NFV9_FIELD_OUT_BYTES:
			DecodeUNumber(v, &(flowMessage.Bytes))
		case netflow.NFV9_FIELD_OUT_PKTS:
			DecodeUNumber(v, &(flowMessage.Packets))

		// L4
		case netflow.NFV9_FIELD_L4_SRC_PORT:
			DecodeUNumber(v, &(flowMessage.SrcPort))
		case netflow.NFV9_FIELD_L4_DST_PORT:
			DecodeUNumber(v, &(flowMessage.DstPort))
		case netflow.NFV9_FIELD_PROTOCOL:
			DecodeUNumber(v, &(flowMessage.Proto))

		// Network
		case netflow.NFV9_FIELD_SRC_AS:
			DecodeUNumber(v, &(flowMessage.SrcAs))
		case netflow.NFV9_FIELD_DST_AS:
			DecodeUNumber(v, &(flowMessage.DstAs))

		// Interfaces
		case netflow.NFV9_FIELD_INPUT_SNMP:
			DecodeUNumber(v, &(flowMessage.InIf))
		case netflow.NFV9_FIELD_OUTPUT_SNMP:
			DecodeUNumber(v, &(flowMessage.OutIf))

		case netflow.NFV9_FIELD_FORWARDING_STATUS:
			DecodeUNumber(v, &(flowMessage.ForwardingStatus))
		case netflow.NFV9_FIELD_SRC_TOS:
			DecodeUNumber(v, &(flowMessage.IpTos))
		case netflow.NFV9_FIELD_TCP_FLAGS:
			DecodeUNumber(v, &(flowMessage.TcpFlags))
		case netflow.NFV9_FIELD_MIN_TTL:
			DecodeUNumber(v, &(flowMessage.IpTtl))

		// IP
		case netflow.NFV9_FIELD_IP_PROTOCOL_VERSION:
			if len(v) > 0 {
				if v[0] == 4 {
					flowMessage.Etype = 0x800
				} else if v[0] == 6 {
					flowMessage.Etype = 0x86dd
				}
			}

		case netflow.NFV9_FIELD_IPV4_SRC_ADDR:
			addrReplaceCheck(&(flowMessage.SrcAddr), v, &(flowMessage.Etype), false)

		case netflow.NFV9_FIELD_IPV4_DST_ADDR:
			addrReplaceCheck(&(flowMessage.DstAddr), v, &(flowMessage.Etype), false)

		case netflow.NFV9_FIELD_SRC_MASK:
			DecodeUNumber(v, &(flowMessage.SrcNet))
		case netflow.NFV9_FIELD_DST_MASK:
			DecodeUNumber(v, &(flowMessage.DstNet))

		case netflow.NFV9_FIELD_IPV6_SRC_ADDR:
			addrReplaceCheck(&(flowMessage.SrcAddr), v, &(flowMessage.Etype), true)

		case netflow.NFV9_FIELD_IPV6_DST_ADDR:
			addrReplaceCheck(&(flowMessage.DstAddr), v, &(flowMessage.Etype), true)

		case netflow.NFV9_FIELD_IPV6_SRC_MASK:
			DecodeUNumber(v, &(flowMessage.SrcNet))
		case netflow.NFV9_FIELD_IPV6_DST_MASK:
			DecodeUNumber(v, &(flowMessage.DstNet))

		case netflow.NFV9_FIELD_IPV4_NEXT_HOP:
			flowMessage.NextHop = v
		case netflow.NFV9_FIELD_BGP_IPV4_NEXT_HOP:
			flowMessage.BgpNextHop = v

		case netflow.NFV9_FIELD_IPV6_NEXT_HOP:
			flowMessage.NextHop = v
		case netflow.NFV9_FIELD_BGP_IPV6_NEXT_HOP:
			flowMessage.BgpNextHop = v

		// ICMP
		case netflow.NFV9_FIELD_ICMP_TYPE:
			var icmpTypeCode uint16
			DecodeUNumber(v, &icmpTypeCode)
			flowMessage.IcmpType = uint32(icmpTypeCode >> 8)
			flowMessage.IcmpCode = uint32(icmpTypeCode & 0xff)
		case netflow.IPFIX_FIELD_icmpTypeCodeIPv6:
			var icmpTypeCode uint16
			DecodeUNumber(v, &icmpTypeCode)
			flowMessage.IcmpType = uint32(icmpTypeCode >> 8)
			flowMessage.IcmpCode = uint32(icmpTypeCode & 0xff)
		case netflow.IPFIX_FIELD_icmpTypeIPv4:
			DecodeUNumber(v, &(flowMessage.IcmpType))
		case netflow.IPFIX_FIELD_icmpTypeIPv6:
			DecodeUNumber(v, &(flowMessage.IcmpType))
		case netflow.IPFIX_FIELD_icmpCodeIPv4:
			DecodeUNumber(v, &(flowMessage.IcmpCode))
		case netflow.IPFIX_FIELD_icmpCodeIPv6:
			DecodeUNumber(v, &(flowMessage.IcmpCode))

		// Mac
		case netflow.NFV9_FIELD_IN_SRC_MAC:
			DecodeUNumber(v, &(flowMessage.SrcMac))
		case netflow.NFV9_FIELD_IN_DST_MAC:
			DecodeUNumber(v, &(flowMessage.DstMac))
		case netflow.NFV9_FIELD_OUT_SRC_MAC:
			DecodeUNumber(v, &(flowMessage.SrcMac))
		case netflow.NFV9_FIELD_OUT_DST_MAC:
			DecodeUNumber(v, &(flowMessage.DstMac))

		case netflow.NFV9_FIELD_SRC_VLAN:
			DecodeUNumber(v, &(flowMessage.VlanId))
			DecodeUNumber(v, &(flowMessage.SrcVlan))
		case netflow.NFV9_FIELD_DST_VLAN:
			DecodeUNumber(v, &(flowMessage.DstVlan))

		case netflow.IPFIX_FIELD_ingressVRFID:
			DecodeUNumber(v, &(flowMessage.IngressVrfId))
		case netflow.IPFIX_FIELD_egressVRFID:
			DecodeUNumber(v, &(flowMessage.EgressVrfId))

		case netflow.NFV9_FIELD_IPV4_IDENT:
			DecodeUNumber(v, &(flowMessage.FragmentId))
		case netflow.NFV9_FIELD_FRAGMENT_OFFSET:
			var fragOffset uint32
			DecodeUNumber(v, &fragOffset)
			flowMessage.FragmentOffset |= fragOffset
		case netflow.IPFIX_FIELD_fragmentFlags:
			var ipFlags uint32
			DecodeUNumber(v, &ipFlags)
			flowMessage.FragmentOffset |= ipFlags
		case netflow.NFV9_FIELD_IPV6_FLOW_LABEL:
			DecodeUNumber(v, &(flowMessage.Ipv6FlowLabel))

		case netflow.IPFIX_FIELD_biflowDirection:
			DecodeUNumber(v, &(flowMessage.BiFlowDirection))

		case netflow.NFV9_FIELD_DIRECTION:
			DecodeUNumber(v, &(flowMessage.FlowDirection))

		// MPLS
		case netflow.IPFIX_FIELD_mplsTopLabelStackSection:
			var mplsLabel uint32
			DecodeUNumber(v, &mplsLabel)
			flowMessage.Mpls_1Label = uint32(mplsLabel >> 4)
			flowMessage.HasMpls = true
		case netflow.IPFIX_FIELD_mplsLabelStackSection2:
			var mplsLabel uint32
			DecodeUNumber(v, &mplsLabel)
			flowMessage.Mpls_2Label = uint32(mplsLabel >> 4)
		case netflow.IPFIX_FIELD_mplsLabelStackSection3:
			var mplsLabel uint32
			DecodeUNumber(v, &mplsLabel)
			flowMessage.Mpls_3Label = uint32(mplsLabel >> 4)
		case netflow.IPFIX_FIELD_mplsTopLabelIPv4Address:
			flowMessage.MplsLabelIp = v
		case netflow.IPFIX_FIELD_mplsTopLabelIPv6Address:
			flowMessage.MplsLabelIp = v

		default:
			if version == 9 {
				// NetFlow v9 time works with a differential based on router's uptime
				switch df.Type {
				case netflow.NFV9_FIELD_FIRST_SWITCHED:
					var timeFirstSwitched uint32
					DecodeUNumber(v, &timeFirstSwitched)
					timeDiff := (uptime - timeFirstSwitched)
					flowMessage.TimeFlowStart = uint64(baseTime - timeDiff/1000)
					flowMessage.TimeFlowStartMs = uint64(baseTime)*1000 - uint64(timeDiff)
				case netflow.NFV9_FIELD_LAST_SWITCHED:
					var timeLastSwitched uint32
					DecodeUNumber(v, &timeLastSwitched)
					timeDiff := (uptime - timeLastSwitched)
					flowMessage.TimeFlowEnd = uint64(baseTime - timeDiff/1000)
					flowMessage.TimeFlowEndMs = uint64(baseTime)*1000 - uint64(timeDiff)
				}
			} else if version == 10 {
				switch df.Type {
				case netflow.IPFIX_FIELD_flowStartSeconds:
					DecodeUNumber(v, &time)
					flowMessage.TimeFlowStart = time
					flowMessage.TimeFlowStartMs = time * 1000
				case netflow.IPFIX_FIELD_flowStartMilliseconds:
					DecodeUNumber(v, &time)
					flowMessage.TimeFlowStart = time / 1000
					flowMessage.TimeFlowStartMs = time
				case netflow.IPFIX_FIELD_flowStartMicroseconds:
					DecodeUNumber(v, &time)
					flowMessage.TimeFlowStart = time / 1000000
					flowMessage.TimeFlowStartMs = time / 1000
				case netflow.IPFIX_FIELD_flowStartNanoseconds:
					DecodeUNumber(v, &time)
					flowMessage.TimeFlowStart = time / 1000000000
					flowMessage.TimeFlowStartMs = time / 1000000
				case netflow.IPFIX_FIELD_flowEndSeconds:
					DecodeUNumber(v, &time)
					flowMessage.TimeFlowEnd = time
					flowMessage.TimeFlowEndMs = time * 1000
				case netflow.IPFIX_FIELD_flowEndMilliseconds:
					DecodeUNumber(v, &time)
					flowMessage.TimeFlowEnd = time / 1000
					flowMessage.TimeFlowEndMs = time
				case netflow.IPFIX_FIELD_flowEndMicroseconds:
					DecodeUNumber(v, &time)
					flowMessage.TimeFlowEnd = time / 1000000
					flowMessage.TimeFlowEndMs = time / 1000
				case netflow.IPFIX_FIELD_flowEndNanoseconds:
					DecodeUNumber(v, &time)
					flowMessage.TimeFlowEnd = time / 1000000000
					flowMessage.TimeFlowEndMs = time / 1000000
				case netflow.IPFIX_FIELD_flowStartDeltaMicroseconds:
					DecodeUNumber(v, &time)
					flowMessage.TimeFlowStart = uint64(baseTime) - time/1000000
					flowMessage.TimeFlowStartMs = uint64(baseTime)*1000 - time/1000
				case netflow.IPFIX_FIELD_flowEndDeltaMicroseconds:
					DecodeUNumber(v, &time)
					flowMessage.TimeFlowEnd = uint64(baseTime) - time/1000000
					flowMessage.TimeFlowEndMs = uint64(baseTime)*1000 - time/1000
				// RFC7133
				case netflow.IPFIX_FIELD_dataLinkFrameSize:
					DecodeUNumber(v, &(flowMessage.Bytes))
					flowMessage.Packets = 1
				case netflow.IPFIX_FIELD_dataLinkFrameSection:
					ParseEthernetHeader(flowMessage, v, mapperSFlow)
					flowMessage.Packets = 1
					if flowMessage.Bytes == 0 {
						flowMessage.Bytes = uint64(len(v))
					}
				}
			}
		}

	}

	return flowMessage
}

func SearchNetFlowDataSetsRecords(version uint16, baseTime uint32, uptime uint32, dataRecords []netflow.DataRecord, mapperNetFlow *NetFlowMapper, mapperSFlow *SFlowMapper) []*flowmessage.FlowMessage {
	var flowMessageSet []*flowmessage.FlowMessage
	for _, record := range dataRecords {
		fmsg := ConvertNetFlowDataSet(version, baseTime, uptime, record.Values, mapperNetFlow, mapperSFlow)
		if fmsg != nil {
			flowMessageSet = append(flowMessageSet, fmsg)
		}
	}
	return flowMessageSet
}

func SearchNetFlowDataSets(version uint16, baseTime uint32, uptime uint32, dataFlowSet []netflow.DataFlowSet, mapperNetFlow *NetFlowMapper, mapperSFlow *SFlowMapper) []*flowmessage.FlowMessage {
	var flowMessageSet []*flowmessage.FlowMessage
	for _, dataFlowSetItem := range dataFlowSet {
		fmsg := SearchNetFlowDataSetsRecords(version, baseTime, uptime, dataFlowSetItem.Records, mapperNetFlow, mapperSFlow)
		if fmsg != nil {
			flowMessageSet = append(flowMessageSet, fmsg...)
		}
	}
	return flowMessageSet
}

func SearchNetFlowOptionDataSets(dataFlowSet []netflow.OptionsDataFlowSet) (uint32, bool) {
	var samplingRate uint32
	var found bool
	for _, dataFlowSetItem := range dataFlowSet {
		for _, record := range dataFlowSetItem.Records {
			b := NetFlowPopulate(record.OptionsValues, 305, &samplingRate)
			if b {
				return samplingRate, b
			}
			b = NetFlowPopulate(record.OptionsValues, 50, &samplingRate)
			if b {
				return samplingRate, b
			}
			b = NetFlowPopulate(record.OptionsValues, 34, &samplingRate)
			if b {
				return samplingRate, b
			}
		}
	}
	return samplingRate, found
}

func SplitNetFlowSets(packetNFv9 netflow.NFv9Packet) ([]netflow.DataFlowSet, []netflow.TemplateFlowSet, []netflow.NFv9OptionsTemplateFlowSet, []netflow.OptionsDataFlowSet) {
	var dataFlowSet []netflow.DataFlowSet
	var templatesFlowSet []netflow.TemplateFlowSet
	var optionsTemplatesFlowSet []netflow.NFv9OptionsTemplateFlowSet
	var optionsDataFlowSet []netflow.OptionsDataFlowSet
	for _, flowSet := range packetNFv9.FlowSets {
		switch tFlowSet := flowSet.(type) {
		case netflow.TemplateFlowSet:
			templatesFlowSet = append(templatesFlowSet, tFlowSet)
		case netflow.NFv9OptionsTemplateFlowSet:
			optionsTemplatesFlowSet = append(optionsTemplatesFlowSet, tFlowSet)
		case netflow.DataFlowSet:
			dataFlowSet = append(dataFlowSet, tFlowSet)
		case netflow.OptionsDataFlowSet:
			optionsDataFlowSet = append(optionsDataFlowSet, tFlowSet)
		}
	}
	return dataFlowSet, templatesFlowSet, optionsTemplatesFlowSet, optionsDataFlowSet
}

func SplitIPFIXSets(packetIPFIX netflow.IPFIXPacket) ([]netflow.DataFlowSet, []netflow.TemplateFlowSet, []netflow.IPFIXOptionsTemplateFlowSet, []netflow.OptionsDataFlowSet) {
	var dataFlowSet []netflow.DataFlowSet
	var templatesFlowSet []netflow.TemplateFlowSet
	var optionsTemplatesFlowSet []netflow.IPFIXOptionsTemplateFlowSet
	var optionsDataFlowSet []netflow.OptionsDataFlowSet
	for _, flowSet := range packetIPFIX.FlowSets {
		switch tFlowSet := flowSet.(type) {
		case netflow.TemplateFlowSet:
			templatesFlowSet = append(templatesFlowSet, tFlowSet)
		case netflow.IPFIXOptionsTemplateFlowSet:
			optionsTemplatesFlowSet = append(optionsTemplatesFlowSet, tFlowSet)
		case netflow.DataFlowSet:
			dataFlowSet = append(dataFlowSet, tFlowSet)
		case netflow.OptionsDataFlowSet:
			optionsDataFlowSet = append(optionsDataFlowSet, tFlowSet)
		}
	}
	return dataFlowSet, templatesFlowSet, optionsTemplatesFlowSet, optionsDataFlowSet
}

func ProcessMessageNetFlow(msgDec interface{}, samplingRateSys SamplingRateSystem) ([]*flowmessage.FlowMessage, error) {
	return ProcessMessageNetFlowConfig(msgDec, samplingRateSys, nil)
}

// Convert a NetFlow datastructure to a FlowMessage protobuf
// Does not put sampling rate
func ProcessMessageNetFlowConfig(msgDec interface{}, samplingRateSys SamplingRateSystem, config *ProducerConfigMapped) ([]*flowmessage.FlowMessage, error) {
	seqnum := uint32(0)
	var baseTime uint32
	var uptime uint32

	var flowMessageSet []*flowmessage.FlowMessage

	switch msgDecConv := msgDec.(type) {
	case netflow.NFv9Packet:
		dataFlowSet, _, _, optionDataFlowSet := SplitNetFlowSets(msgDecConv)

		seqnum = msgDecConv.SequenceNumber
		baseTime = msgDecConv.UnixSeconds
		uptime = msgDecConv.SystemUptime
		obsDomainId := msgDecConv.SourceId

		var cfg *NetFlowMapper
		if config != nil {
			cfg = config.NetFlowV9
		}
		flowMessageSet = SearchNetFlowDataSets(9, baseTime, uptime, dataFlowSet, cfg, nil)
		samplingRate, found := SearchNetFlowOptionDataSets(optionDataFlowSet)
		if samplingRateSys != nil {
			if found {
				samplingRateSys.AddSamplingRate(9, obsDomainId, samplingRate)
			} else {
				samplingRate, _ = samplingRateSys.GetSamplingRate(9, obsDomainId)
			}
		}
		for _, fmsg := range flowMessageSet {
			fmsg.SequenceNum = seqnum
			fmsg.SamplingRate = uint64(samplingRate)
		}
	case netflow.IPFIXPacket:
		dataFlowSet, _, _, optionDataFlowSet := SplitIPFIXSets(msgDecConv)

		seqnum = msgDecConv.SequenceNumber
		baseTime = msgDecConv.ExportTime
		obsDomainId := msgDecConv.ObservationDomainId

		var cfgIpfix *NetFlowMapper
		var cfgSflow *SFlowMapper
		if config != nil {
			cfgIpfix = config.IPFIX
			cfgSflow = config.SFlow
		}
		flowMessageSet = SearchNetFlowDataSets(10, baseTime, uptime, dataFlowSet, cfgIpfix, cfgSflow)

		samplingRate, found := SearchNetFlowOptionDataSets(optionDataFlowSet)
		if samplingRateSys != nil {
			if found {
				samplingRateSys.AddSamplingRate(10, obsDomainId, samplingRate)
			} else {
				samplingRate, _ = samplingRateSys.GetSamplingRate(10, obsDomainId)
			}
		}
		for _, fmsg := range flowMessageSet {
			fmsg.SequenceNum = seqnum
			fmsg.SamplingRate = uint64(samplingRate)
			fmsg.ObservationDomainId = obsDomainId
		}
	default:
		return flowMessageSet, errors.New("Bad NetFlow/IPFIX version")
	}

	return flowMessageSet, nil
}
