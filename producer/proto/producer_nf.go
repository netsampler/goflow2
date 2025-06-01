package protoproducer

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
	"github.com/netsampler/goflow2/v2/decoders/utils"
	flowmessage "github.com/netsampler/goflow2/v2/pb"
	"github.com/netsampler/goflow2/v2/producer"
)

type SamplingRateSystem interface {
	GetSamplingRate(version uint16, obsDomainId uint32) (uint32, error)
	AddSamplingRate(version uint16, obsDomainId uint32, samplingRate uint32)
}

type basicSamplingRateKey struct {
	version     uint16
	obsDomainId uint32
}

type basicSamplingRateSystem struct {
	sampling     map[basicSamplingRateKey]uint32
	samplinglock *sync.RWMutex
}

func CreateSamplingSystem() SamplingRateSystem {
	ts := &basicSamplingRateSystem{
		sampling:     make(map[basicSamplingRateKey]uint32),
		samplinglock: &sync.RWMutex{},
	}
	return ts
}

func (s *basicSamplingRateSystem) AddSamplingRate(version uint16, obsDomainId uint32, samplingRate uint32) {
	s.samplinglock.Lock()
	defer s.samplinglock.Unlock()
	s.sampling[basicSamplingRateKey{
		version:     version,
		obsDomainId: obsDomainId,
	}] = samplingRate
}

func (s *basicSamplingRateSystem) GetSamplingRate(version uint16, obsDomainId uint32) (uint32, error) {
	s.samplinglock.RLock()
	defer s.samplinglock.RUnlock()
	if samplingRate, ok := s.sampling[basicSamplingRateKey{
		version:     version,
		obsDomainId: obsDomainId,
	}]; ok {
		return samplingRate, nil
	}

	return 0, fmt.Errorf("sampling rate not found")
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

func NetFlowPopulate(dataFields []netflow.DataField, typeId uint16, addr interface{}) (bool, error) {
	exists, value := NetFlowLookFor(dataFields, typeId)
	if exists && value != nil {
		valueBytes, ok := value.([]byte)
		valueReader := bytes.NewBuffer(valueBytes)
		if ok {
			switch addrt := addr.(type) {
			//case *(net.IP):
			//	*addrt = valueBytes
			case *(time.Time):
				t := uint64(0)
				if err := utils.BinaryRead(valueReader, binary.BigEndian, &t); err != nil {
					return false, err
				}
				t64 := int64(t / 1000)
				*addrt = time.Unix(t64, 0)
			default:
				if err := utils.BinaryRead(valueReader, binary.BigEndian, addr); err != nil {
					return false, err
				}
			}
		}
	}
	return exists, nil
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
		return fmt.Errorf("the parameter is not a pointer to a byte/uint16/uint32/uint64 structure")
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
		return fmt.Errorf("the parameter is not a pointer to a int8/int16/int32/int64 structure")
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
			return fmt.Errorf("non-regular number of bytes for a number: %v", l)
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
			return fmt.Errorf("non-regular number of bytes for a number: %v", l)
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
			return fmt.Errorf("non-regular number of bytes for a number: %v", l)
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
			return fmt.Errorf("non-regular number of bytes for a number: %v", l)
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

func ConvertNTPEpoch(ntpTime uint64) uint64 {
	seconds := ntpTime >> 32
	seconds -= 2208988800

	fraction := float64(ntpTime&0xffffffff) * 1.0e9 / (1 << 32)
	return seconds*1e9 + uint64(fraction)
}

func ConvertNetFlowDataSet(flowMessage *ProtoProducerMessage, version uint16, baseTime uint32, uptime uint32, record []netflow.DataField, mapperNetFlow TemplateMapper, mapperSFlow PacketMapper) error {
	var time uint64
	baseTimeNs := uint64(baseTime) * 1000000000
	// the following should be overriden if the template contains timing information
	// otherwise, defaults to the export time
	flowMessage.TimeFlowStartNs = baseTimeNs
	flowMessage.TimeFlowEndNs = baseTimeNs

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

		if err := MapCustomNetFlow(flowMessage, df, mapperNetFlow); err != nil {
			return err
		}

		if df.PenProvided {
			continue
		}

		switch df.Type {

		case netflow.IPFIX_FIELD_observationPointId:
			if err := DecodeUNumber(v, &(flowMessage.ObservationPointId)); err != nil {
				return err
			}

		// Statistics
		case netflow.NFV9_FIELD_IN_BYTES:
			if err := DecodeUNumber(v, &(flowMessage.Bytes)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_IN_PKTS:
			if err := DecodeUNumber(v, &(flowMessage.Packets)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_OUT_BYTES:
			if err := DecodeUNumber(v, &(flowMessage.Bytes)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_OUT_PKTS:
			if err := DecodeUNumber(v, &(flowMessage.Packets)); err != nil {
				return err
			}

		// L4
		case netflow.NFV9_FIELD_L4_SRC_PORT:
			if err := DecodeUNumber(v, &(flowMessage.SrcPort)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_L4_DST_PORT:
			if err := DecodeUNumber(v, &(flowMessage.DstPort)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_PROTOCOL:
			if err := DecodeUNumber(v, &(flowMessage.Proto)); err != nil {
				return err
			}

		// Network
		case netflow.NFV9_FIELD_SRC_AS:
			if err := DecodeUNumber(v, &(flowMessage.SrcAs)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_DST_AS:
			if err := DecodeUNumber(v, &(flowMessage.DstAs)); err != nil {
				return err
			}

		// Interfaces
		case netflow.NFV9_FIELD_INPUT_SNMP:
			if err := DecodeUNumber(v, &(flowMessage.InIf)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_OUTPUT_SNMP:
			if err := DecodeUNumber(v, &(flowMessage.OutIf)); err != nil {
				return err
			}

		case netflow.NFV9_FIELD_FORWARDING_STATUS:
			if err := DecodeUNumber(v, &(flowMessage.ForwardingStatus)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_SRC_TOS:
			if err := DecodeUNumber(v, &(flowMessage.IpTos)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_TCP_FLAGS:
			if err := DecodeUNumber(v, &(flowMessage.TcpFlags)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_MIN_TTL:
			if err := DecodeUNumber(v, &(flowMessage.IpTtl)); err != nil {
				return err
			}

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
			if err := DecodeUNumber(v, &(flowMessage.SrcNet)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_DST_MASK:
			if err := DecodeUNumber(v, &(flowMessage.DstNet)); err != nil {
				return err
			}

		case netflow.NFV9_FIELD_IPV6_SRC_ADDR:
			addrReplaceCheck(&(flowMessage.SrcAddr), v, &(flowMessage.Etype), true)

		case netflow.NFV9_FIELD_IPV6_DST_ADDR:
			addrReplaceCheck(&(flowMessage.DstAddr), v, &(flowMessage.Etype), true)

		case netflow.NFV9_FIELD_IPV6_SRC_MASK:
			if err := DecodeUNumber(v, &(flowMessage.SrcNet)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_IPV6_DST_MASK:
			if err := DecodeUNumber(v, &(flowMessage.DstNet)); err != nil {
				return err
			}

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
			if err := DecodeUNumber(v, &icmpTypeCode); err != nil {
				return err
			}
			flowMessage.IcmpType = uint32(icmpTypeCode >> 8)
			flowMessage.IcmpCode = uint32(icmpTypeCode & 0xff)
		case netflow.IPFIX_FIELD_icmpTypeCodeIPv6:
			var icmpTypeCode uint16
			if err := DecodeUNumber(v, &icmpTypeCode); err != nil {
				return err
			}
			flowMessage.IcmpType = uint32(icmpTypeCode >> 8)
			flowMessage.IcmpCode = uint32(icmpTypeCode & 0xff)
		case netflow.IPFIX_FIELD_icmpTypeIPv4:
			if err := DecodeUNumber(v, &(flowMessage.IcmpType)); err != nil {
				return err
			}
		case netflow.IPFIX_FIELD_icmpTypeIPv6:
			if err := DecodeUNumber(v, &(flowMessage.IcmpType)); err != nil {
				return err
			}
		case netflow.IPFIX_FIELD_icmpCodeIPv4:
			if err := DecodeUNumber(v, &(flowMessage.IcmpCode)); err != nil {
				return err
			}
		case netflow.IPFIX_FIELD_icmpCodeIPv6:
			if err := DecodeUNumber(v, &(flowMessage.IcmpCode)); err != nil {
				return err
			}

		// Mac
		case netflow.NFV9_FIELD_IN_SRC_MAC:
			if err := DecodeUNumber(v, &(flowMessage.SrcMac)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_IN_DST_MAC:
			if err := DecodeUNumber(v, &(flowMessage.DstMac)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_OUT_SRC_MAC:
			if err := DecodeUNumber(v, &(flowMessage.SrcMac)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_OUT_DST_MAC:
			if err := DecodeUNumber(v, &(flowMessage.DstMac)); err != nil {
				return err
			}

		case netflow.NFV9_FIELD_SRC_VLAN:
			if err := DecodeUNumber(v, &(flowMessage.VlanId)); err != nil {
				return err
			}
			if err := DecodeUNumber(v, &(flowMessage.SrcVlan)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_DST_VLAN:
			if err := DecodeUNumber(v, &(flowMessage.DstVlan)); err != nil {
				return err
			}

		case netflow.NFV9_FIELD_IPV4_IDENT:
			if err := DecodeUNumber(v, &(flowMessage.FragmentId)); err != nil {
				return err
			}
		case netflow.NFV9_FIELD_FRAGMENT_OFFSET:
			var fragOffset uint32
			if err := DecodeUNumber(v, &fragOffset); err != nil {
				return err
			}
			flowMessage.FragmentOffset = fragOffset
		case netflow.IPFIX_FIELD_fragmentFlags:
			var ipFlags uint32
			if err := DecodeUNumber(v, &ipFlags); err != nil {
				return err
			}
			flowMessage.IpFlags = ipFlags >> 5
		case netflow.NFV9_FIELD_IPV6_FLOW_LABEL:
			if err := DecodeUNumber(v, &(flowMessage.Ipv6FlowLabel)); err != nil {
				return err
			}

		// MPLS
		case netflow.IPFIX_FIELD_mplsTopLabelStackSection:
			var mplsLabel uint32
			if err := DecodeUNumber(v, &mplsLabel); err != nil {
				return err
			}
			if len(flowMessage.MplsLabel) < 1 {
				flowMessage.MplsLabel = make([]uint32, 1)
			}
			flowMessage.MplsLabel[0] = uint32(mplsLabel >> 4)
		case netflow.IPFIX_FIELD_mplsLabelStackSection2:
			var mplsLabel uint32
			if err := DecodeUNumber(v, &mplsLabel); err != nil {
				return err
			}
			if len(flowMessage.MplsLabel) < 2 {
				tmpLabels := make([]uint32, 2)
				copy(tmpLabels, flowMessage.MplsLabel)
				flowMessage.MplsLabel = tmpLabels
			}
			flowMessage.MplsLabel[1] = uint32(mplsLabel >> 4)
		case netflow.IPFIX_FIELD_mplsLabelStackSection3:
			var mplsLabel uint32
			if err := DecodeUNumber(v, &mplsLabel); err != nil {
				return err
			}
			if len(flowMessage.MplsLabel) < 3 {
				tmpLabels := make([]uint32, 3)
				copy(tmpLabels, flowMessage.MplsLabel)
				flowMessage.MplsLabel = tmpLabels
			}
			flowMessage.MplsLabel[2] = uint32(mplsLabel >> 4)
		case netflow.IPFIX_FIELD_mplsTopLabelIPv4Address:
			flowMessage.MplsIp = append(flowMessage.MplsIp, v)
		case netflow.IPFIX_FIELD_mplsTopLabelIPv6Address:
			flowMessage.MplsIp = append(flowMessage.MplsIp, v)

		default:
			if version == 9 {
				// NetFlow v9 time works with a differential based on router's uptime
				uptimeNs := uint64(uptime) * 1e6 // uptime is in milliseconds in NetFlow v9, converts to nanoseconds
				switch df.Type {
				case netflow.NFV9_FIELD_FIRST_SWITCHED:
					var timeFirstSwitched uint32
					if err := DecodeUNumber(v, &timeFirstSwitched); err != nil {
						return err
					}
					timeDiff := (uptimeNs - uint64(timeFirstSwitched)*1e6)
					flowMessage.TimeFlowStartNs = baseTimeNs - timeDiff
				case netflow.NFV9_FIELD_LAST_SWITCHED:
					var timeLastSwitched uint32
					if err := DecodeUNumber(v, &timeLastSwitched); err != nil {
						return err
					}
					timeDiff := (uptimeNs - uint64(timeLastSwitched)*1e6)
					flowMessage.TimeFlowEndNs = baseTimeNs - timeDiff
				}
			} else if version == 10 {
				switch df.Type {
				case netflow.IPFIX_FIELD_flowStartSeconds:
					if err := DecodeUNumber(v, &time); err != nil {
						return err
					}
					flowMessage.TimeFlowStartNs = time * 1000000000
				case netflow.IPFIX_FIELD_flowStartMilliseconds:
					if err := DecodeUNumber(v, &time); err != nil {
						return err
					}
					flowMessage.TimeFlowStartNs = time * 1000000
				case netflow.IPFIX_FIELD_flowStartMicroseconds:
					if err := DecodeUNumber(v, &time); err != nil {
						return err
					}
					flowMessage.TimeFlowStartNs = ConvertNTPEpoch(time)
				case netflow.IPFIX_FIELD_flowStartNanoseconds:
					if err := DecodeUNumber(v, &time); err != nil {
						return err
					}
					flowMessage.TimeFlowStartNs = ConvertNTPEpoch(time)
				case netflow.IPFIX_FIELD_flowEndSeconds:
					if err := DecodeUNumber(v, &time); err != nil {
						return err
					}
					flowMessage.TimeFlowEndNs = time * 1000000000
				case netflow.IPFIX_FIELD_flowEndMilliseconds:
					if err := DecodeUNumber(v, &time); err != nil {
						return err
					}
					flowMessage.TimeFlowEndNs = time * 1000000
				case netflow.IPFIX_FIELD_flowEndMicroseconds:
					if err := DecodeUNumber(v, &time); err != nil {
						return err
					}
					flowMessage.TimeFlowEndNs = ConvertNTPEpoch(time)
				case netflow.IPFIX_FIELD_flowEndNanoseconds:
					if err := DecodeUNumber(v, &time); err != nil {
						return err
					}
					flowMessage.TimeFlowEndNs = ConvertNTPEpoch(time)
				case netflow.IPFIX_FIELD_flowStartDeltaMicroseconds:
					if err := DecodeUNumber(v, &time); err != nil {
						return err
					}
					flowMessage.TimeFlowStartNs = baseTimeNs - time*1000
				case netflow.IPFIX_FIELD_flowEndDeltaMicroseconds:
					if err := DecodeUNumber(v, &time); err != nil {
						return err
					}
					flowMessage.TimeFlowEndNs = baseTimeNs - time*1000
				// RFC7133
				case netflow.IPFIX_FIELD_dataLinkFrameSize:
					if err := DecodeUNumber(v, &(flowMessage.Bytes)); err != nil {
						return err
					}
					flowMessage.Packets = 1
				case netflow.IPFIX_FIELD_dataLinkFrameSection:
					if err := mapperSFlow.ParsePacket(flowMessage, v); err != nil {
						return err
					}
					flowMessage.Packets = 1
					if flowMessage.Bytes == 0 {
						flowMessage.Bytes = uint64(len(v))
					}
				}
			}
		}

	}
	return nil
}

func SearchNetFlowDataSetsRecords(version uint16, baseTime uint32, uptime uint32, dataRecords []netflow.DataRecord, mapperNetFlow TemplateMapper, mapperSFlow PacketMapper) (flowMessageSet []producer.ProducerMessage, err error) {
	for _, record := range dataRecords {
		fmsg := protoMessagePool.Get().(*ProtoProducerMessage)
		fmsg.Reset()
		if err := ConvertNetFlowDataSet(fmsg, version, baseTime, uptime, record.Values, mapperNetFlow, mapperSFlow); err != nil {
			return flowMessageSet, err
		}
		if fmsg != nil {
			flowMessageSet = append(flowMessageSet, fmsg)
		}
	}
	return flowMessageSet, nil
}

func SearchNetFlowDataSets(version uint16, baseTime uint32, uptime uint32, dataFlowSet []netflow.DataFlowSet, mapperNetFlow TemplateMapper, mapperSFlow PacketMapper) (flowMessageSet []producer.ProducerMessage, err error) {
	for _, dataFlowSetItem := range dataFlowSet {
		fmsg, err := SearchNetFlowDataSetsRecords(version, baseTime, uptime, dataFlowSetItem.Records, mapperNetFlow, mapperSFlow)
		if err != nil {
			return flowMessageSet, err
		}
		if fmsg != nil {
			flowMessageSet = append(flowMessageSet, fmsg...)
		}
	}
	return flowMessageSet, nil
}

func SearchNetFlowOptionDataSets(dataFlowSet []netflow.OptionsDataFlowSet) (samplingRate uint32, found bool, err error) {
	for _, dataFlowSetItem := range dataFlowSet {
		for _, record := range dataFlowSetItem.Records {
			if found, err := NetFlowPopulate(record.OptionsValues, 305, &samplingRate); err != nil || found {
				return samplingRate, found, err
			}
			if found, err := NetFlowPopulate(record.OptionsValues, 50, &samplingRate); err != nil || found {
				return samplingRate, found, err
			}
			if found, err := NetFlowPopulate(record.OptionsValues, 34, &samplingRate); err != nil || found {
				return samplingRate, found, err
			}
		}
	}
	return samplingRate, found, err
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

// Convert a NetFlow datastructure to a FlowMessage protobuf
// Does not put sampling rate
func ProcessMessageIPFIXConfig(packet *netflow.IPFIXPacket, samplingRateSys SamplingRateSystem, config ProtoProducerConfig) (flowMessageSet []producer.ProducerMessage, err error) {
	dataFlowSet, _, _, optionDataFlowSet := SplitIPFIXSets(*packet)

	seqnum := packet.SequenceNumber
	baseTime := packet.ExportTime
	obsDomainId := packet.ObservationDomainId

	var cfgIpfix TemplateMapper
	var cfgSflow PacketMapper
	if config != nil {
		cfgIpfix = config.GetIPFIXMapper()
		cfgSflow = config.GetPacketMapper()
	}
	flowMessageSet, err = SearchNetFlowDataSets(10, baseTime, 0, dataFlowSet, cfgIpfix, cfgSflow)
	if err != nil {
		return flowMessageSet, err
	}

	samplingRate, found, err := SearchNetFlowOptionDataSets(optionDataFlowSet)
	if err != nil {
		return flowMessageSet, err
	}
	if samplingRateSys != nil {
		if found {
			samplingRateSys.AddSamplingRate(10, obsDomainId, samplingRate)
		} else {
			samplingRate, _ = samplingRateSys.GetSamplingRate(10, obsDomainId)
		}
	}
	for _, msg := range flowMessageSet {
		fmsg, ok := msg.(*ProtoProducerMessage)
		if !ok {
			continue
		}
		fmsg.SequenceNum = seqnum
		fmsg.SamplingRate = uint64(samplingRate)
		fmsg.ObservationDomainId = obsDomainId
	}
	return flowMessageSet, nil
}

// Convert a NetFlow datastructure to a FlowMessage protobuf
// Does not put sampling rate
func ProcessMessageNetFlowV9Config(packet *netflow.NFv9Packet, samplingRateSys SamplingRateSystem, config ProtoProducerConfig) (flowMessageSet []producer.ProducerMessage, err error) {
	dataFlowSet, _, _, optionDataFlowSet := SplitNetFlowSets(*packet)

	seqnum := packet.SequenceNumber
	baseTime := packet.UnixSeconds
	uptime := packet.SystemUptime
	obsDomainId := packet.SourceId

	var cfgNetFlow TemplateMapper
	if config != nil {
		cfgNetFlow = config.GetNetFlowMapper()
	}
	flowMessageSet, err = SearchNetFlowDataSets(9, baseTime, uptime, dataFlowSet, cfgNetFlow, nil)
	if err != nil {
		return flowMessageSet, err
	}
	samplingRate, found, err := SearchNetFlowOptionDataSets(optionDataFlowSet)
	if err != nil {
		return flowMessageSet, err
	}
	if samplingRateSys != nil {
		if found {
			samplingRateSys.AddSamplingRate(9, obsDomainId, samplingRate)
		} else {
			samplingRate, _ = samplingRateSys.GetSamplingRate(9, obsDomainId)
		}
	}
	for _, msg := range flowMessageSet {
		fmsg, ok := msg.(*ProtoProducerMessage)
		if !ok {
			continue
		}
		fmsg.SequenceNum = seqnum
		fmsg.SamplingRate = uint64(samplingRate)
		fmsg.ObservationDomainId = obsDomainId
	}
	return flowMessageSet, nil
}
