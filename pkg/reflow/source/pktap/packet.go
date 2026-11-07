// Package pktap decodes Apple's packet-tap framing independently of libpcap.
package pktap

import "github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"

const flagV2 = 0x00080000

func normalize(frame capture.Frame) capture.Frame { return capture.NormalizePktap(frame) }
