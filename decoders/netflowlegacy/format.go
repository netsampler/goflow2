package netflowlegacy

import (
	"fmt"
	"time"
)

func (p PacketNetFlowV5) MarshalJSON() ([]byte, error) {
	return []byte("todo"), nil
}

func (p *PacketNetFlowV5) String() string {
	return fmt.Sprintf("NetFlowV%d seq:%d count:%d", p.Version, p.FlowSequence, p.Count)
}

func (p PacketNetFlowV5) String2() string {
	str := "NetFlow v5 Packet\n"
	str += "-----------------\n"
	str += fmt.Sprintf("  Version: %v\n", p.Version)
	str += fmt.Sprintf("  Count:  %v\n", p.Count)

	unixSeconds := time.Unix(int64(p.UnixSecs), int64(p.UnixNSecs))
	str += fmt.Sprintf("  SystemUptime: %v\n", time.Duration(p.SysUptime)*time.Millisecond)
	str += fmt.Sprintf("  UnixSeconds: %v\n", unixSeconds.String())
	str += fmt.Sprintf("  FlowSequence: %v\n", p.FlowSequence)
	str += fmt.Sprintf("  EngineType: %v\n", p.EngineType)
	str += fmt.Sprintf("  EngineId: %v\n", p.EngineId)
	str += fmt.Sprintf("  SamplingInterval: %v\n", p.SamplingInterval)
	str += fmt.Sprintf("  Records (%v):\n", len(p.Records))

	for i, record := range p.Records {
		str += fmt.Sprintf("    Record %v:\n", i)
		str += record.String()
	}
	return str
}

func IPToString(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d", ip>>24, (ip>>16)&0xFF, (ip>>8)&0xFF, ip&0xFF)
}

func (r RecordsNetFlowV5) String() string {
	str := fmt.Sprintf("      SrcAddr: %v\n", IPToString(r.SrcAddr))
	str += fmt.Sprintf("      DstAddr: %v\n", IPToString(r.DstAddr))
	str += fmt.Sprintf("      NextHop: %v\n", IPToString(r.NextHop))
	str += fmt.Sprintf("      Input: %v\n", r.Input)
	str += fmt.Sprintf("      Output: %v\n", r.Output)
	str += fmt.Sprintf("      DPkts: %v\n", r.DPkts)
	str += fmt.Sprintf("      DOctets: %v\n", r.DOctets)
	str += fmt.Sprintf("      First: %v\n", time.Duration(r.First)*time.Millisecond)
	str += fmt.Sprintf("      Last: %v\n", time.Duration(r.Last)*time.Millisecond)
	str += fmt.Sprintf("      SrcPort: %v\n", r.SrcPort)
	str += fmt.Sprintf("      DstPort: %v\n", r.DstPort)
	str += fmt.Sprintf("      TCPFlags: %v\n", r.TCPFlags)
	str += fmt.Sprintf("      Proto: %v\n", r.Proto)
	str += fmt.Sprintf("      Tos: %v\n", r.Tos)
	str += fmt.Sprintf("      SrcAS: %v\n", r.SrcAS)
	str += fmt.Sprintf("      DstAS: %v\n", r.DstAS)
	str += fmt.Sprintf("      SrcMask: %v\n", r.SrcMask)
	str += fmt.Sprintf("      DstMask: %v\n", r.DstMask)

	return str
}
