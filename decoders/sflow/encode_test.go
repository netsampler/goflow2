package sflow

import (
	"bytes"
	"testing"

	"github.com/netsampler/goflow2/v3/decoders/utils"
	"github.com/stretchr/testify/assert"
)

func TestEncodeDecodeSFlow(t *testing.T) {
	packet := Packet{
		Version:   5,
		IPVersion: 1,
		AgentIP:   utils.IPAddress{192, 0, 2, 1},
		SubAgentId:     1,
		SequenceNumber: 2,
		Uptime:         3,
		Samples: []interface{}{
			FlowSample{
				Header: SampleHeader{
					Format:              SAMPLE_FORMAT_FLOW,
					SampleSequenceNumber: 42,
					SourceIdType:         0,
					SourceIdValue:        7,
				},
				SamplingRate:     10,
				SamplePool:       20,
				Drops:            0,
				Input:            1,
				Output:           2,
				FlowRecordsCount: 1,
				Records: []FlowRecord{
					{
						Header: RecordHeader{
							DataFormat: FLOW_TYPE_RAW,
						},
						Data: SampledHeader{
							Protocol:       1,
							FrameLength:    64,
							Stripped:       0,
							OriginalLength: 64,
							HeaderData:     []byte{0xde, 0xad, 0xbe, 0xef},
						},
					},
				},
			},
		},
	}

	encoded, err := EncodeMessage(&packet)
	assert.NoError(t, err)

	var decoded Packet
	assert.NoError(t, DecodeMessageVersion(bytes.NewBuffer(encoded), &decoded))
	assert.Equal(t, uint32(5), decoded.Version)
	assert.Equal(t, uint32(1), decoded.IPVersion)
	assert.Equal(t, utils.IPAddress{192, 0, 2, 1}, decoded.AgentIP)
	assert.Len(t, decoded.Samples, 1)

	sample, ok := decoded.Samples[0].(FlowSample)
	assert.True(t, ok)
	assert.Equal(t, uint32(42), sample.Header.SampleSequenceNumber)
	assert.Equal(t, uint32(7), sample.Header.SourceIdValue)
	assert.Len(t, sample.Records, 1)

	record := sample.Records[0]
	assert.Equal(t, uint32(FLOW_TYPE_RAW), record.Header.DataFormat)
	header, ok := record.Data.(SampledHeader)
	assert.True(t, ok)
	assert.Equal(t, []byte{0xde, 0xad, 0xbe, 0xef}, header.HeaderData)
}

func TestEncodeDecodeSFlowExpandedFlowSample(t *testing.T) {
	packet := Packet{
		Version:        5,
		IPVersion:      2,
		AgentIP:        utils.IPAddress{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		SubAgentId:     10,
		SequenceNumber: 11,
		Uptime:         12,
		Samples: []interface{}{
			ExpandedFlowSample{
				Header: SampleHeader{
					Format:               SAMPLE_FORMAT_EXPANDED_FLOW,
					SampleSequenceNumber: 100,
					SourceIdType:         2,
					SourceIdValue:        99,
				},
				SamplingRate:     400,
				SamplePool:       500,
				Drops:            2,
				InputIfFormat:    1,
				InputIfValue:     10,
				OutputIfFormat:   2,
				OutputIfValue:    20,
				FlowRecordsCount: 3,
				Records: []FlowRecord{
					{
						Data: SampledIPv4{
							SampledIPBase: SampledIPBase{
								Length:   60,
								Protocol: 6,
								SrcIP:    utils.IPAddress{192, 0, 2, 10},
								DstIP:    utils.IPAddress{198, 51, 100, 20},
								SrcPort:  12345,
								DstPort:  443,
								TcpFlags: 0x12,
							},
							Tos: 16,
						},
					},
					{
						Data: ExtendedSwitch{
							SrcVlan:     100,
							SrcPriority: 1,
							DstVlan:     200,
							DstPriority: 2,
						},
					},
					{
						Data: ExtendedGateway{
							NextHopIPVersion:  1,
							NextHop:           utils.IPAddress{203, 0, 113, 1},
							AS:                64512,
							SrcAS:             64513,
							SrcPeerAS:         64514,
							ASDestinations:    1,
							ASPathType:        1,
							ASPathLength:      2,
							ASPath:            []uint32{64515, 64516},
							CommunitiesLength: 2,
							Communities:       []uint32{100, 200},
							LocalPref:         300,
						},
					},
				},
			},
		},
	}

	encoded, err := EncodeMessage(&packet)
	assert.NoError(t, err)

	var decoded Packet
	assert.NoError(t, DecodeMessageVersion(bytes.NewBuffer(encoded), &decoded))
	assert.Equal(t, uint32(2), decoded.IPVersion)
	assert.Len(t, decoded.Samples, 1)

	sample, ok := decoded.Samples[0].(ExpandedFlowSample)
	assert.True(t, ok)
	assert.Equal(t, uint32(100), sample.Header.SampleSequenceNumber)
	assert.Equal(t, uint32(99), sample.Header.SourceIdValue)
	assert.Equal(t, uint32(3), sample.FlowRecordsCount)
	assert.Len(t, sample.Records, 3)

	ipv4, ok := sample.Records[0].Data.(SampledIPv4)
	assert.True(t, ok)
	assert.Equal(t, utils.IPAddress{192, 0, 2, 10}, ipv4.SrcIP)
	assert.Equal(t, uint32(443), ipv4.DstPort)

	sw, ok := sample.Records[1].Data.(ExtendedSwitch)
	assert.True(t, ok)
	assert.Equal(t, uint32(200), sw.DstVlan)

	gw, ok := sample.Records[2].Data.(ExtendedGateway)
	assert.True(t, ok)
	assert.Equal(t, utils.IPAddress{203, 0, 113, 1}, gw.NextHop)
	assert.Equal(t, []uint32{64515, 64516}, gw.ASPath)
	assert.Equal(t, []uint32{100, 200}, gw.Communities)
}

func TestEncodeDecodeSFlowCounterSample(t *testing.T) {
	packet := Packet{
		Version:        5,
		IPVersion:      1,
		AgentIP:        utils.IPAddress{198, 51, 100, 1},
		SubAgentId:     1,
		SequenceNumber: 2,
		Uptime:         3,
		Samples: []interface{}{
			CounterSample{
				Header: SampleHeader{
					Format:               SAMPLE_FORMAT_COUNTER,
					SampleSequenceNumber: 7,
					SourceIdType:         0,
					SourceIdValue:        8,
				},
				CounterRecordsCount: 2,
				Records: []CounterRecord{
					{
						Data: IfCounters{
							IfIndex:            1,
							IfType:             2,
							IfSpeed:            1000,
							IfDirection:        1,
							IfStatus:           3,
							IfInOctets:         100,
							IfInUcastPkts:      10,
							IfInMulticastPkts:  11,
							IfInBroadcastPkts:  12,
							IfInDiscards:       13,
							IfInErrors:         14,
							IfInUnknownProtos:  15,
							IfOutOctets:        200,
							IfOutUcastPkts:     20,
							IfOutMulticastPkts: 21,
							IfOutBroadcastPkts: 22,
							IfOutDiscards:      23,
							IfOutErrors:        24,
							IfPromiscuousMode:  1,
						},
					},
					{
						Data: EthernetCounters{
							Dot3StatsAlignmentErrors:           1,
							Dot3StatsFCSErrors:                 2,
							Dot3StatsSingleCollisionFrames:     3,
							Dot3StatsMultipleCollisionFrames:   4,
							Dot3StatsSQETestErrors:             5,
							Dot3StatsDeferredTransmissions:     6,
							Dot3StatsLateCollisions:            7,
							Dot3StatsExcessiveCollisions:       8,
							Dot3StatsInternalMacTransmitErrors: 9,
							Dot3StatsCarrierSenseErrors:        10,
							Dot3StatsFrameTooLongs:             11,
							Dot3StatsInternalMacReceiveErrors:  12,
							Dot3StatsSymbolErrors:              13,
						},
					},
				},
			},
		},
	}

	encoded, err := EncodeMessage(&packet)
	assert.NoError(t, err)

	var decoded Packet
	assert.NoError(t, DecodeMessageVersion(bytes.NewBuffer(encoded), &decoded))
	assert.Len(t, decoded.Samples, 1)

	sample, ok := decoded.Samples[0].(CounterSample)
	assert.True(t, ok)
	assert.Equal(t, uint32(2), sample.CounterRecordsCount)
	assert.Len(t, sample.Records, 2)

	ifc, ok := sample.Records[0].Data.(IfCounters)
	assert.True(t, ok)
	assert.Equal(t, uint64(1000), ifc.IfSpeed)
	assert.Equal(t, uint32(24), ifc.IfOutErrors)

	eth, ok := sample.Records[1].Data.(EthernetCounters)
	assert.True(t, ok)
	assert.Equal(t, uint32(13), eth.Dot3StatsSymbolErrors)
}

func TestEncodeDecodeSFlowDropSample(t *testing.T) {
	packet := Packet{
		Version:        5,
		IPVersion:      1,
		AgentIP:        utils.IPAddress{192, 168, 119, 184},
		SubAgentId:     100000,
		SequenceNumber: 3,
		Uptime:         12350,
		Samples: []interface{}{
			DropSample{
				Header: SampleHeader{
					Format:               SAMPLE_FORMAT_DROP,
					SampleSequenceNumber: 2,
					SourceIdType:         0,
					SourceIdValue:        1,
				},
				Drops:            256,
				Input:            1,
				Output:           2,
				Reason:           1,
				FlowRecordsCount: 3,
				Records: []FlowRecord{
					{Data: EgressQueue{Queue: 42}},
					{Data: ExtendedACL{Number: 7, Name: "foo!", Direction: 2}},
					{Data: ExtendedFunction{Symbol: "dropper"}},
				},
			},
		},
	}

	encoded, err := EncodeMessage(&packet)
	assert.NoError(t, err)

	var decoded Packet
	assert.NoError(t, DecodeMessageVersion(bytes.NewBuffer(encoded), &decoded))
	assert.Len(t, decoded.Samples, 1)

	sample, ok := decoded.Samples[0].(DropSample)
	assert.True(t, ok)
	assert.Equal(t, uint32(1), sample.Reason)
	assert.Len(t, sample.Records, 3)
	assert.Equal(t, EgressQueue{Queue: 42}, sample.Records[0].Data)
	assert.Equal(t, ExtendedACL{Number: 7, Name: "foo!", Direction: 2}, sample.Records[1].Data)
	assert.Equal(t, ExtendedFunction{Symbol: "dropper"}, sample.Records[2].Data)
}
