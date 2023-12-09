package producer

import (
	"fmt"
	"runtime/debug"
)

var (
	ProducerError = fmt.Errorf("producer error")
)

type ProducerErrorMessage struct {
	Msg interface{}
	Inner string
	Stacktrace []byte
}

func (e *ProducerErrorMessage) Error() string {
	return fmt.Sprintf("%s", e.Inner)
}

func (e *ProducerErrorMessage) Unwrap() []error {
	return []error{ProducerError,}
}

type PanicProducerWrapper struct {
	wrapped ProducerInterface
}

func (p *PanicProducerWrapper) Produce(msg interface{}, args *ProduceArgs) (flowMessageSet []ProducerMessage, err error) {

	defer func() {
		if pErr := recover(); pErr != nil {

			pErrC, _ := pErr.(string)
			err = &ProducerErrorMessage{Msg:msg, Inner: pErrC, Stacktrace: debug.Stack()}
		}
	}()

	flowMessageSet, err = p.wrapped.Produce(msg, args)
	return flowMessageSet, err
}


func (p *PanicProducerWrapper) Close() {
	p.wrapped.Close()
}

func (p *PanicProducerWrapper) Commit(flowMessageSet []ProducerMessage) {
	p.wrapped.Commit(flowMessageSet)
}

func WrapPanicProducer(wrapped ProducerInterface) ProducerInterface {
	return &PanicProducerWrapper{
		wrapped: wrapped,
	}
}
