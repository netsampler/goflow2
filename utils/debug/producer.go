package debug

import (
	"runtime/debug"

	"github.com/netsampler/goflow2/v2/producer"
)

// PanicProducerWrapper wraps a producer to recover panics during Produce.
type PanicProducerWrapper struct {
	wrapped producer.ProducerInterface
}

// Produce calls the wrapped producer and converts panics into errors.
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

// Close forwards Close to the wrapped producer.
func (p *PanicProducerWrapper) Close() {
	p.wrapped.Close()
}

// Commit forwards Commit to the wrapped producer.
func (p *PanicProducerWrapper) Commit(flowMessageSet []producer.ProducerMessage) {
	p.wrapped.Commit(flowMessageSet)
}

// WrapPanicProducer wraps a producer to recover panics as errors.
func WrapPanicProducer(wrapped producer.ProducerInterface) producer.ProducerInterface {
	return &PanicProducerWrapper{
		wrapped: wrapped,
	}
}
