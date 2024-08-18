package debug

import (
	"runtime/debug"

	"github.com/netsampler/goflow2/v2/producer"
)

type PanicProducerWrapper struct {
	wrapped producer.ProducerInterface
}

func (p *PanicProducerWrapper) Produce(msg interface{}, args *producer.ProduceArgs) (flowMessageSet []producer.ProducerMessage, err error) {

	defer func() {
		if pErr := recover(); pErr != nil {
			pErrC, _ := pErr.(string)
			err = &PanicErrorMessage{Msg: msg, Inner: pErrC, Stacktrace: debug.Stack()}
		}
	}()

	flowMessageSet, err = p.wrapped.Produce(msg, args)
	return flowMessageSet, err
}

func (p *PanicProducerWrapper) Close() {
	p.wrapped.Close()
}

func (p *PanicProducerWrapper) Commit(flowMessageSet []producer.ProducerMessage) {
	p.wrapped.Commit(flowMessageSet)
}

func WrapPanicProducer(wrapped producer.ProducerInterface) producer.ProducerInterface {
	return &PanicProducerWrapper{
		wrapped: wrapped,
	}
}
