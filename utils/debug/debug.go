package debug

import (
	"fmt"
)

var (
	ErrPanic = fmt.Errorf("panic")
)

type PanicErrorMessage struct {
	Msg        interface{}
	Inner      string
	Stacktrace []byte
}

func (e *PanicErrorMessage) Error() string {
	return e.Inner
}

func (e *PanicErrorMessage) Unwrap() []error {
	return []error{ErrPanic}
}
