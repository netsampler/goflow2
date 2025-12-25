// Package debug provides panic-wrapping helpers for decoders and producers.
package debug

import (
	"fmt"
)

var (
	// PanicError marks a recovered panic.
	PanicError = fmt.Errorf("panic")
)

// PanicErrorMessage captures a recovered panic with stacktrace.
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
