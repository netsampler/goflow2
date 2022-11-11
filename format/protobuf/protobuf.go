package protobuf

import (
	"context"
	"flag"
	"fmt"
	"net"

	"github.com/golang/protobuf/proto"
	"github.com/netsampler/goflow2/format"
	"github.com/netsampler/goflow2/format/common"
	flowmessage "github.com/netsampler/goflow2/pb"
)

type ProtobufDriver struct {
	fixedLen      bool
	convertToIPV6 bool
}

func (d *ProtobufDriver) Prepare() error {
	common.HashFlag()
	flag.BoolVar(&d.fixedLen, "format.protobuf.fixedlen", false, "Prefix the protobuf with message length")
	flag.BoolVar(&d.convertToIPV6, "format.protobuf.toipv6", false, "Convert all addresses to IPV6 format on the wire")
	return nil
}

func (d *ProtobufDriver) Init(context.Context) error {
	return common.ManualHashInit()
}

func (d *ProtobufDriver) Format(data interface{}) ([]byte, []byte, error) {
	msg, ok := data.(proto.Message)
	if !ok {
		return nil, nil, fmt.Errorf("message is not protobuf")
	}
	key := common.HashProtoLocal(msg)
	if d.convertToIPV6 {
		if fm, ok := msg.(*flowmessage.FlowMessage); !ok {
			return nil, nil, fmt.Errorf("message is not a flowmessage")
		} else {
			convertAddressesToIPV6(fm)
		}

	}
	if !d.fixedLen {
		b, err := proto.Marshal(msg)
		return []byte(key), b, err
	} else {
		buf := proto.NewBuffer([]byte{})
		err := buf.EncodeMessage(msg)
		return []byte(key), buf.Bytes(), err
	}
}

func init() {
	d := &ProtobufDriver{}
	format.RegisterFormatDriver("pb", d)
}

func convertAddressesToIPV6(flowMessage *flowmessage.FlowMessage) {
	flowMessage.SrcAddr = net.IP(flowMessage.SrcAddr).To16()
	flowMessage.DstAddr = net.IP(flowMessage.DstAddr).To16()
	flowMessage.NextHop = net.IP(flowMessage.NextHop).To16()
	flowMessage.SamplerAddress = net.IP(flowMessage.SamplerAddress).To16()
	flowMessage.BgpNextHop = net.IP(flowMessage.BgpNextHop).To16()
}
