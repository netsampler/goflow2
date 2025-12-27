// Package debug provides panic-wrapping helpers for decoders and producers.
package debug

import (
	"fmt"
)

var (
	// ErrPanic marks a recovered panic.
	ErrPanic = fmt.Errorf("panic")
)

// PanicErrorMessage captures a recovered panic with stacktrace.
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
