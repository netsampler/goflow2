package protoproducer

import (
	"testing"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
	"github.com/netsampler/goflow2/v2/decoders/sflow"
	"github.com/stretchr/testify/assert"
)

func TestProcessMessageNetFlow(t *testing.T) {
	records := []netflow.DataRecord{
		netflow.DataRecord{
			Values: []netflow.DataField{
				netflow.DataField{
					Type:  netflow.NFV9_FIELD_IPV4_SRC_ADDR,
					Value: []byte{10, 0, 0, 1},
				},
				netflow.DataField{
					Type: netflow.NFV9_FIELD_FIRST_SWITCHED,
					// 218432176
					Value: []byte{0x0d, 0x05, 0x02, 0xb0},
				},
				netflow.DataField{
					Type: netflow.NFV9_FIELD_LAST_SWITCHED,
					// 218432192
					Value: []byte{0x0d, 0x05, 0x02, 0xc0},
				},
				netflow.DataField{
					Type: netflow.NFV9_FIELD_MPLS_LABEL_1,
					// 24041
					Value: []byte{0x05, 0xde, 0x94},
				},
				netflow.DataField{
					Type: netflow.NFV9_FIELD_MPLS_LABEL_2,
					// 211992
					Value: []byte{0x33, 0xc1, 0x85},
				},
				netflow.DataField{
					Type: netflow.NFV9_FIELD_MPLS_LABEL_3,
					// 48675
					Value: []byte{0x0b, 0xe2, 0x35},
				},
			},
		},
	}
	dfs := []interface{}{
		netflow.DataFlowSet{
			Records: records,
		},
	}

	pktnf9 := netflow.NFv9Packet{
		SystemUptime: 218432000,
		UnixSeconds:  1705732882,
		FlowSets:     dfs,
	}
	testsr := &SingleSamplingRateSystem{1}
	msgs, err := ProcessMessageNetFlowV9Config(&pktnf9, testsr, nil)
	if assert.Nil(t, err) && assert.Len(t, msgs, 1) {
		msg, ok := msgs[0].(*ProtoProducerMessage)
		if assert.True(t, ok) {
			assert.Equal(t, uint64(1705732882176*1e6), msg.TimeFlowStartNs)
			assert.Equal(t, uint64(1705732882192*1e6), msg.TimeFlowEndNs)
			assert.Equal(t, []uint32{24041, 211992, 48675}, msg.MplsLabel)
		}
	}

	pktipfix := netflow.IPFIXPacket{
		FlowSets: dfs,
	}
	_, err = ProcessMessageIPFIXConfig(&pktipfix, testsr, nil)
	assert.Nil(t, err)
}

func TestProcessMessageSFlow(t *testing.T) {
	sh := sflow.SampledHeader{
		FrameLength: 10,
		Protocol:    1,
		HeaderData: []byte{
			0xff, 0xab, 0xcd, 0xef, 0xab, 0xcd, 0xff, 0xab, 0xcd, 0xef, 0xab, 0xbc, 0x86, 0xdd, 0x60, 0x2e,
			0xc4, 0xec, 0x01, 0xcc, 0x06, 0x40, 0xfd, 0x01, 0x00, 0x00, 0xff, 0x01, 0x82, 0x10, 0xcd, 0xff,
			0xff, 0x1c, 0x00, 0x00, 0x01, 0x50, 0xfd, 0x01, 0x00, 0x00, 0xff, 0x01, 0x00, 0x01, 0x02, 0xff,
			0xff, 0x93, 0x00, 0x00, 0x02, 0x46, 0xcf, 0xca, 0x00, 0x50, 0x05, 0x15, 0x21, 0x6f, 0xa4, 0x9c,
			0xf4, 0x59, 0x80, 0x18, 0x08, 0x09, 0x8c, 0x86, 0x00, 0x00, 0x01, 0x01, 0x08, 0x0a, 0x2a, 0x85,
			0xee, 0x9e, 0x64, 0x5c, 0x27, 0x28,
		},
	}
	pkt := sflow.Packet{
		Version: 5,
		Samples: []interface{}{
			sflow.FlowSample{
				SamplingRate: 1,
				Records: []sflow.FlowRecord{
					sflow.FlowRecord{
						Data: sh,
					},
				},
			},
			sflow.ExpandedFlowSample{
				SamplingRate: 1,
				Records: []sflow.FlowRecord{
					sflow.FlowRecord{
						Data: sh,
					},
				},
			},
		},
	}
	_, err := ProcessMessageSFlowConfig(&pkt, nil)
	assert.Nil(t, err)
}

func TestExpandedSFlowDecode(t *testing.T) {
	flowMessages, err := ProcessMessageSFlowConfig(getSflowPacket(), nil)
	flowMessageIf := flowMessages[0]
	flowMessage := flowMessageIf.(*ProtoProducerMessage)

	assert.Nil(t, err)

	assert.Equal(t, []byte{0x05, 0x05, 0x05, 0x05}, flowMessage.BgpNextHop)
	assert.Equal(t, []uint32{3936619448, 3936619708, 3936623548}, flowMessage.BgpCommunities)
	assert.Equal(t, []uint32{456}, flowMessage.AsPath)
	assert.Equal(t, []byte{0x09, 0x09, 0x09, 0x09}, flowMessage.NextHop)
}

func getSflowPacket() *sflow.Packet {
	pkt := sflow.Packet{
		Version:        5,
		IPVersion:      1,
		AgentIP:        []uint8{1, 2, 3, 4},
		SubAgentId:     0,
		SequenceNumber: 3178205882,
		Uptime:         3011091704,
		SamplesCount:   1,
		Samples: []interface{}{
			sflow.FlowSample{
				Header: sflow.SampleHeader{
					Format:               1,
					Length:               662,
					SampleSequenceNumber: 2757962272,
					SourceIdType:         0,
					SourceIdValue:        1000100,
				},
				SamplingRate:     16383,
				SamplePool:       639948256,
				Drops:            0,
				Input:            1000100,
				Output:           1000005,
				FlowRecordsCount: 4,
				Records: []sflow.FlowRecord{
					sflow.FlowRecord{
						Header: sflow.RecordHeader{
							DataFormat: 1001,
							Length:     16,
						},
						Data: sflow.ExtendedSwitch{
							SrcVlan:     952,
							SrcPriority: 0,
							DstVlan:     952,
							DstPriority: 0,
						},
					},
					sflow.FlowRecord{
						Header: sflow.RecordHeader{
							DataFormat: 1,
							Length:     144,
						},
						Data: sflow.SampledHeader{
							Protocol:       1,
							FrameLength:    1522,
							Stripped:       4,
							OriginalLength: 128,
							HeaderData: []byte{
								0x74, 0x83, 0xef, 0x2e, 0xc3, 0xc5, 0xac, 0x1f, 0x6b, 0x2c, 0x43, 0x36, 0x81, 0x00, 0x03, 0xb8,
								0x08, 0x00, 0x45, 0x00, 0x05, 0xdc, 0x59, 0xa5, 0x40, 0x00, 0x40, 0x06, 0x0a, 0xb8, 0xb9, 0x3b,
								0xdf, 0xb6, 0x32, 0x44, 0x05, 0x89, 0x23, 0x78, 0xc9, 0x06, 0x24, 0x6c, 0x0b, 0xf4, 0xd9, 0xce,
								0x9c, 0x66, 0x50, 0x10, 0x00, 0x1e, 0x29, 0x8a, 0x00, 0x00, 0xb4, 0x7e, 0xb7, 0xfd, 0x16, 0x3e,
								0x19, 0x97, 0xa8, 0xb4, 0x2a, 0xf7, 0x49, 0x96, 0xf4, 0x0e, 0xef, 0xa7, 0x55, 0x93, 0x27, 0x6f,
								0x1e, 0x20, 0xe1, 0x04, 0x2f, 0x36, 0x18, 0xfe, 0x7b, 0x88, 0x1f, 0xc9, 0x57, 0xbc, 0x71, 0x43,
								0x3d, 0x1c, 0x6c, 0xb0, 0x3d, 0xf7, 0x51, 0x48, 0x68, 0x94, 0x47, 0x00, 0xd3, 0x1a, 0x9d, 0xdb,
								0x2f, 0x1e, 0x39, 0xcf, 0xfd, 0x96, 0x79, 0xdf, 0xb0, 0x2d, 0x02, 0x6e, 0x72, 0xf5, 0x29, 0x73,
							},
						},
					},
					sflow.FlowRecord{
						Header: sflow.RecordHeader{
							DataFormat: 1003,
							Length:     56,
						},
						Data: sflow.ExtendedGateway{
							NextHopIPVersion:  1,
							NextHop:           []uint8{5, 5, 5, 5},
							AS:                123,
							SrcAS:             0,
							SrcPeerAS:         0,
							ASDestinations:    1,
							ASPathType:        2,
							ASPathLength:      1,
							ASPath:            []uint32{456},
							CommunitiesLength: 3,
							Communities: []uint32{
								3936619448,
								3936619708,
								3936623548,
							},
							LocalPref: 170,
						},
					},
					sflow.FlowRecord{
						Header: sflow.RecordHeader{
							DataFormat: 1002,
							Length:     16,
						},
						Data: sflow.ExtendedRouter{
							NextHopIPVersion: 1,
							NextHop:          []uint8{9, 9, 9, 9},
							SrcMaskLen:       26,
							DstMaskLen:       22,
						},
					},
				},
			},
		},
	}
	return &pkt
}

func TestNetFlowV9Time(t *testing.T) {
	// This test ensures the NetFlow v9 timestamps are properly calculated.
	// It passes a baseTime = 2024-01-01 00:00:00 (in seconds) and an uptime of 2 seconds  (in milliseconds).
	// The flow record was logged at 1 second of uptime (in milliseconds).
	// The calculation is the following: baseTime - uptime + flowUptime.
	var flowMessage ProtoProducerMessage
	err := ConvertNetFlowDataSet(&flowMessage, 9, 1704067200, 2000, []netflow.DataField{
		netflow.DataField{
			Type:  netflow.NFV9_FIELD_FIRST_SWITCHED,
			Value: []byte{0x0, 0x0, 0x03, 0xe8}, // 1000
		},
	}, nil, nil)
	assert.Nil(t, err)
	assert.Equal(t, uint64(1704067199)*1e9, flowMessage.TimeFlowStartNs)
}

func TestConvertNTPEpoch(t *testing.T) {
	e := ConvertNTPEpoch(0xebe50e38c50cc000)
	assert.Equal(t, uint64(1748668344769725799), e)
}
