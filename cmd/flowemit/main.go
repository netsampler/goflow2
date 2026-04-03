package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/decoders/utils"
)

func main() {
	flowAddrFlag := flag.String("flow-addr", "127.0.0.1:2055", "UDP destination for NetFlow/IPFIX")
	sflowAddrFlag := flag.String("sflow-addr", "127.0.0.1:6343", "UDP destination for sFlow")
	intervalFlag := flag.Duration("interval", 5*time.Second, "send interval")
	flag.Parse()

	flowAddr, err := net.ResolveUDPAddr("udp", *flowAddrFlag)
	if err != nil {
		log.Fatalf("resolve flow addr: %v", err)
	}
	sflowAddr, err := net.ResolveUDPAddr("udp", *sflowAddrFlag)
	if err != nil {
		log.Fatalf("resolve sflow addr: %v", err)
	}

	flowConn, err := net.DialUDP("udp", nil, flowAddr)
	if err != nil {
		log.Fatalf("dial flow udp: %v", err)
	}
	defer flowConn.Close()

	sflowConn, err := net.DialUDP("udp", nil, sflowAddr)
	if err != nil {
		log.Fatalf("dial sflow udp: %v", err)
	}
	defer sflowConn.Close()

	log.Printf(
		"sending NetFlow v5, NetFlow v9, and IPFIX to %s; sFlow to %s every %s",
		flowAddr.String(),
		sflowAddr.String(),
		intervalFlag.String(),
	)

	sendOnce := func() {
		now := time.Now().UTC()
		flowPackets := []struct {
			name string
			data []byte
		}{
			{name: "netflowv5", data: mustEncodeNetFlowV5(now)},
			{name: "netflowv9", data: mustEncodeNetFlowV9(now)},
			{name: "ipfix", data: mustEncodeIPFIX(now)},
		}

		for _, pkt := range flowPackets {
			n, err := flowConn.Write(pkt.data)
			if err != nil {
				log.Printf("send %s: %v", pkt.name, err)
				continue
			}
			log.Printf("sent %s packet (%d bytes)", pkt.name, n)
		}

		sflowData := mustEncodeSFlow(now)
		n, err := sflowConn.Write(sflowData)
		if err != nil {
			log.Printf("send sflow: %v", err)
			return
		}
		log.Printf("sent sflow packet (%d bytes)", n)
	}

	sendOnce()

	ticker := time.NewTicker(*intervalFlag)
	defer ticker.Stop()

	for range ticker.C {
		sendOnce()
	}
}

func mustEncodeNetFlowV5(now time.Time) []byte {
	packet := &netflowlegacy.PacketNetFlowV5{
		Version:          5,
		SysUptime:        123456,
		UnixSecs:         uint32(now.Unix()),
		UnixNSecs:        uint32(now.Nanosecond()),
		FlowSequence:     uint32(now.Unix()),
		EngineType:       1,
		EngineId:         1,
		SamplingInterval: 100,
		Records: []netflowlegacy.RecordsNetFlowV5{
			{
				SrcAddr:  netflowlegacy.IPAddress(0xc0000201),
				DstAddr:  netflowlegacy.IPAddress(0xc6336401),
				NextHop:  netflowlegacy.IPAddress(0x00000000),
				Input:    10,
				Output:   20,
				DPkts:    5,
				DOctets:  600,
				First:    123400,
				Last:     123456,
				SrcPort:  12345,
				DstPort:  443,
				TCPFlags: 0x12,
				Proto:    6,
				Tos:      0,
				SrcAS:    64512,
				DstAS:    64513,
				SrcMask:  24,
				DstMask:  24,
			},
		},
	}

	data, err := netflowlegacy.EncodeMessage(packet)
	if err != nil {
		log.Fatalf("encode netflow v5: %v", err)
	}
	return data
}

func mustEncodeNetFlowV9(now time.Time) []byte {
	packet := netflow.NFv9Packet{
		Version:        9,
		Count:          4,
		SystemUptime:   123456,
		UnixSeconds:    uint32(now.Unix()),
		SequenceNumber: uint32(now.Unix()),
		SourceId:       256,
		FlowSets: []interface{}{
			netflow.TemplateFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 0},
				Records: []netflow.TemplateRecord{
					{
						TemplateId: 256,
						FieldCount: 7,
						Fields: []netflow.Field{
							{Type: netflow.NFV9_FIELD_IN_BYTES, Length: 4},
							{Type: netflow.NFV9_FIELD_IN_PKTS, Length: 4},
							{Type: netflow.NFV9_FIELD_PROTOCOL, Length: 1},
							{Type: netflow.NFV9_FIELD_IPV4_SRC_ADDR, Length: 4},
							{Type: netflow.NFV9_FIELD_IPV4_DST_ADDR, Length: 4},
							{Type: netflow.NFV9_FIELD_L4_SRC_PORT, Length: 2},
							{Type: netflow.NFV9_FIELD_L4_DST_PORT, Length: 2},
						},
					},
				},
			},
			netflow.NFv9OptionsTemplateFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 1},
				Records: []netflow.NFv9OptionsTemplateRecord{
					{
						TemplateId:   257,
						ScopeLength:  4,
						OptionLength: 4,
						Scopes: []netflow.Field{
							{Type: 1, Length: 4},
						},
						Options: []netflow.Field{
							{Type: netflow.NFV9_FIELD_SAMPLING_INTERVAL, Length: 4},
						},
					},
				},
			},
			netflow.DataFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 256},
				Records: []netflow.DataRecord{
					{
						Values: []netflow.DataField{
							{Type: netflow.NFV9_FIELD_IN_BYTES, Value: []byte{0x00, 0x00, 0x02, 0x58}},
							{Type: netflow.NFV9_FIELD_IN_PKTS, Value: []byte{0x00, 0x00, 0x00, 0x05}},
							{Type: netflow.NFV9_FIELD_PROTOCOL, Value: []byte{0x06}},
							{Type: netflow.NFV9_FIELD_IPV4_SRC_ADDR, Value: []byte{192, 0, 2, 1}},
							{Type: netflow.NFV9_FIELD_IPV4_DST_ADDR, Value: []byte{198, 51, 100, 1}},
							{Type: netflow.NFV9_FIELD_L4_SRC_PORT, Value: []byte{0x30, 0x39}},
							{Type: netflow.NFV9_FIELD_L4_DST_PORT, Value: []byte{0x01, 0xbb}},
						},
					},
				},
			},
			netflow.OptionsDataFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 257},
				Records: []netflow.OptionsDataRecord{
					{
						ScopesValues: []netflow.DataField{
							{Type: 1, Value: []byte{0x00, 0x00, 0x00, 0x01}},
						},
						OptionsValues: []netflow.DataField{
							{Type: netflow.NFV9_FIELD_SAMPLING_INTERVAL, Value: []byte{0x00, 0x00, 0x03, 0xe8}},
						},
					},
				},
			},
		},
	}

	data, err := netflow.EncodeMessage(&packet)
	if err != nil {
		log.Fatalf("encode netflow v9: %v", err)
	}
	return data
}

func mustEncodeIPFIX(now time.Time) []byte {
	packet := netflow.IPFIXPacket{
		Version:             10,
		ExportTime:          uint32(now.Unix()),
		SequenceNumber:      uint32(now.Unix()),
		ObservationDomainId: 512,
		FlowSets: []interface{}{
			netflow.TemplateFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 2},
				Records: []netflow.TemplateRecord{
					{
						TemplateId: 300,
						FieldCount: 7,
						Fields: []netflow.Field{
							{Type: netflow.IPFIX_FIELD_octetDeltaCount, Length: 8},
							{Type: netflow.IPFIX_FIELD_packetDeltaCount, Length: 8},
							{Type: netflow.IPFIX_FIELD_protocolIdentifier, Length: 1},
							{Type: netflow.IPFIX_FIELD_sourceIPv4Address, Length: 4},
							{Type: netflow.IPFIX_FIELD_destinationIPv4Address, Length: 4},
							{Type: netflow.IPFIX_FIELD_sourceTransportPort, Length: 2},
							{Type: netflow.IPFIX_FIELD_destinationTransportPort, Length: 2},
						},
					},
				},
			},
			netflow.IPFIXOptionsTemplateFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 3},
				Records: []netflow.IPFIXOptionsTemplateRecord{
					{
						TemplateId:      301,
						FieldCount:      2,
						ScopeFieldCount: 1,
						Scopes: []netflow.Field{
							{Type: netflow.IPFIX_FIELD_observationDomainId, Length: 4},
						},
						Options: []netflow.Field{
							{Type: netflow.IPFIX_FIELD_samplingInterval, Length: 4},
						},
					},
				},
			},
			netflow.DataFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 300},
				Records: []netflow.DataRecord{
					{
						Values: []netflow.DataField{
							{Type: netflow.IPFIX_FIELD_octetDeltaCount, Value: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x58}},
							{Type: netflow.IPFIX_FIELD_packetDeltaCount, Value: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x05}},
							{Type: netflow.IPFIX_FIELD_protocolIdentifier, Value: []byte{0x06}},
							{Type: netflow.IPFIX_FIELD_sourceIPv4Address, Value: []byte{192, 0, 2, 1}},
							{Type: netflow.IPFIX_FIELD_destinationIPv4Address, Value: []byte{198, 51, 100, 1}},
							{Type: netflow.IPFIX_FIELD_sourceTransportPort, Value: []byte{0x30, 0x39}},
							{Type: netflow.IPFIX_FIELD_destinationTransportPort, Value: []byte{0x01, 0xbb}},
						},
					},
				},
			},
			netflow.OptionsDataFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 301},
				Records: []netflow.OptionsDataRecord{
					{
						ScopesValues: []netflow.DataField{
							{Type: netflow.IPFIX_FIELD_observationDomainId, Value: []byte{0x00, 0x00, 0x02, 0x00}},
						},
						OptionsValues: []netflow.DataField{
							{Type: netflow.IPFIX_FIELD_samplingInterval, Value: []byte{0x00, 0x00, 0x03, 0xe8}},
						},
					},
				},
			},
		},
	}

	data, err := netflow.EncodeMessage(&packet)
	if err != nil {
		log.Fatalf("encode ipfix: %v", err)
	}
	return data
}

func mustEncodeSFlow(now time.Time) []byte {
	packet := sflow.Packet{
		Version:        5,
		IPVersion:      1,
		AgentIP:        utils.IPAddress{127, 0, 0, 1},
		SubAgentId:     1,
		SequenceNumber: uint32(now.Unix()),
		Uptime:         123456,
		Samples: []interface{}{
			sflow.FlowSample{
				Header: sflow.SampleHeader{
					Format:               sflow.SAMPLE_FORMAT_FLOW,
					SampleSequenceNumber: uint32(now.Unix()),
					SourceIdType:         0,
					SourceIdValue:        1,
				},
				SamplingRate:     100,
				SamplePool:       1000,
				Drops:            0,
				Input:            10,
				Output:           20,
				FlowRecordsCount: 1,
				Records: []sflow.FlowRecord{
					{
						Data: sflow.SampledHeader{
							Protocol:       1,
							FrameLength:    74,
							Stripped:       0,
							OriginalLength: 54,
							HeaderData: []byte{
								0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
								0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
								0x08, 0x00,
								0x45, 0x00, 0x00, 0x28, 0x12, 0x34, 0x40, 0x00,
								0x40, 0x06, 0x00, 0x00, 0xc0, 0x00, 0x02, 0x01,
								0xc6, 0x33, 0x64, 0x01,
								0x30, 0x39, 0x01, 0xbb,
								0x00, 0x00, 0x00, 0x01,
								0x00, 0x00, 0x00, 0x00,
								0x50, 0x02, 0x20, 0x00,
								0x00, 0x00, 0x00, 0x00,
							},
						},
					},
				},
			},
		},
	}

	data, err := sflow.EncodeMessage(&packet)
	if err != nil {
		log.Fatalf("encode sflow: %v", err)
	}
	return data
}

func init() {
	log.SetFlags(0)
	log.SetPrefix(fmt.Sprintf("[%s] ", "flowemit"))
}
