package queue

import (
	flowpb "github.com/netsampler/goflow2/v2/pb"
)

type MessageWrapper struct {
	SizeMeter

	msg  *flowpb.FlowMessage
	size uint
}

func Wrap(msg *flowpb.FlowMessage, size uint) MessageWrapper {
	return MessageWrapper{msg: msg, size: size}
}

func (m MessageWrapper) Size() uint {
	return m.size
}

func (m MessageWrapper) Message() *flowpb.FlowMessage {
	return m.msg
}
