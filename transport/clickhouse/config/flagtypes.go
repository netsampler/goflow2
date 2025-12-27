package config

import (
	"fmt"
	"strings"
)

type StringSliceFlag []string

func (s *StringSliceFlag) String() string {
	return fmt.Sprintf("%v", []string(*s))
}

// Set supports both single values and comma-separated values
// Examples:
//   -flag value1 -flag value2 (multiple flag calls)
//   -flag "value1,value2,value3" (comma-separated in single call)
//   -flag value1,value2 (comma-separated without quotes)
func (s *StringSliceFlag) Set(value string) error {
	// Split by comma and trim whitespace
	values := strings.Split(value, ",")
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed != "" {
			*s = append(*s, trimmed)
		}
	}
	return nil
}

type Password string

func (p *Password) String() string { return "..." }
func (p *Password) Expose() string { return string(*p) }
func (p *Password) Set(value string) error {
	*p = Password(value)
	return nil
}
