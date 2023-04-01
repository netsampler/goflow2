package producer

import (
	"fmt"
	flowmessage "github.com/netsampler/goflow2/pb"
)

type ProtoProducerMessage flowmessage.FlowMessage

func (m *ProtoProducerMessage) String() string {
	return fmt.Sprintf("test")
}

func (m *ProtoProducerMessage) MarshalJSON() ([]byte, error) {
	// todo: embed the marshalling of format (previously)
	return []byte(fmt.Sprintf("test")), nil
}
