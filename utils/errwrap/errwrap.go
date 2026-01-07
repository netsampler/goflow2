// Package errwrap provides contextual error wrapping helpers.
package errwrap

import (
	"fmt"
	"runtime"
	"strings"
)

// Wrap returns an error annotated with the caller function name.
func Wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", callerName(2), err)
}

// WrapWith returns an error annotated with caller function name and context.
func WrapWith(err error, context string) error {
	if err == nil {
		return nil
	}
	if context == "" {
		return fmt.Errorf("%s: %w", callerName(2), err)
	}
	return fmt.Errorf("%s: %s: %w", callerName(2), context, err)
}

func callerName(skip int) string {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return "unknown"
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "unknown"
	}
	name := fn.Name()
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}
