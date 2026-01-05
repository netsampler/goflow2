package config

import (
	"flag"
	"fmt"
	"strings"

	"github.com/netsampler/goflow2/v3/format"
	"github.com/netsampler/goflow2/v3/transport"
)

// BindCommonFlags registers shared logging/format/transport flags.
func BindCommonFlags(fs *flag.FlagSet, logLevel, logFmt, formatName, transportName *string) {
	fs.StringVar(logLevel, "loglevel", "info", "Log level")
	fs.StringVar(logFmt, "logfmt", "normal", "Log formatter")
	fs.StringVar(formatName, "format", "json", fmt.Sprintf("Choose the format (available: %s)", strings.Join(format.GetFormats(), ", ")))
	fs.StringVar(transportName, "transport", "file", fmt.Sprintf("Choose the transport (available: %s)", strings.Join(transport.GetTransports(), ", ")))
}
