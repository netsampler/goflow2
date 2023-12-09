package debug

import (
	"fmt"
)

var (
	PanicError = fmt.Errorf("panic")
)

type PanicErrorMessage struct {
	Msg        interface{}
	Inner      string
	Stacktrace []byte
}

func (e *PanicErrorMessage) Error() string {
	return fmt.Sprintf("%s", e.Inner)
}

func (e *PanicErrorMessage) Unwrap() []error {
	return []error{PanicError}
}
