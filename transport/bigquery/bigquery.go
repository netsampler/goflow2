package table

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"cloud.google.com/go/bigquery"
	"github.com/netsampler/goflow2/transport"
	"google.golang.org/api/option"
)

type BigQueryDriver struct {
	bigQueryProjectID string
	bigQueryDatasetID string
	bigQueryTableID   string
	bigQueryJsonKey   string
	client            *bigquery.Client
}

func (d *BigQueryDriver) Prepare() error {
	flag.StringVar(&d.bigQueryProjectID, "transport.bigquery.project", "", "BigQuery project ID")
	flag.StringVar(&d.bigQueryDatasetID, "transport.bigquery.dataset", "", "BigQuery dataset ID")
	flag.StringVar(&d.bigQueryTableID, "transport.bigquery.table", "", "BigQuery table ID")
	flag.StringVar(&d.bigQueryJsonKey, "transport.bigquery.jsonkey", "", "BigQuery JSON key file path")
	return nil
}

func (d *BigQueryDriver) Init(ctx context.Context) error {
	client, err := bigquery.NewClient(ctx, d.bigQueryProjectID, option.WithCredentialsFile(d.bigQueryJsonKey))
	d.client = client
	if err != nil {
		return fmt.Errorf("bigquery.NewClient: %w", err)
	}
	return nil
}

type Item struct {
	Type                string
	ObservationPointID  int
	ObservationDomainID int
	TimeReceived        int
	SequenceNum         int
	SamplingRate        int
	SamplerAddress      string
	TimeFlowStart       int
	TimeFlowEnd         int
	TimeFlowStartMs     int
	TimeFlowEndMs       int
	Bytes               int
	Packets             int
	SrcAddr             string
	DstAddr             string
	Etype               int
	Proto               int
	SrcPort             int
	DstPort             int
	InIf                int
	OutIf               int
	SrcMac              string
	DstMac              string
	SrcVlan             int
	DstVlan             int
	VlanId              int
	IngressVrfID        int
	EgressVrfID         int
	IPTos               int
	ForwardingStatus    int
	IPTTL               int
	TCPFlags            int
	IcmpType            int
	IcmpCode            int
	IPv6FlowLabel       int
	FragmentId          int
	FragmentOffset      int
	BiFlowDirection     int
	SrcAS               int
	DstAS               int
	NextHop             string
	NextHopAS           int
	SrcNet              int
	DstNet              int
	EtypeName           string
	ProtoName           string
	IcmpName            string
}

// Save implements the ValueSaver interface.
// This example disables best-effort de-duplication, which allows for higher throughput.
func (i *Item) Save() (map[string]bigquery.Value, string, error) {
	return map[string]bigquery.Value{
		"Type":                i.Type,
		"ObservationPointID":  i.ObservationPointID,
		"ObservationDomainID": i.ObservationDomainID,
		"TimeReceived":        i.TimeReceived,
		"SequenceNum":         i.SequenceNum,
		"SamplingRate":        i.SamplingRate,
		"SamplerAddress":      i.SamplerAddress,
		"TimeFlowStart":       i.TimeFlowStart,
		"TimeFlowEnd":         i.TimeFlowEnd,
		"TimeFlowStartMs":     i.TimeFlowStartMs,
		"TimeFlowEndMs":       i.TimeFlowEndMs,
		"Bytes":               i.Bytes,
		"Packets":             i.Packets,
		"SrcAddr":             i.SrcAddr,
		"DstAddr":             i.DstAddr,
		"Etype":               i.Etype,
		"Proto":               i.Proto,
		"SrcPort":             i.SrcPort,
		"DstPort":             i.DstPort,
		"InIf":                i.InIf,
		"OutIf":               i.OutIf,
		"SrcMac":              i.SrcMac,
		"DstMac":              i.DstMac,
		"SrcVlan":             i.SrcVlan,
		"DstVlan":             i.DstVlan,
		"VlanId":              i.VlanId,
		"IngressVrfID":        i.IngressVrfID,
		"EgressVrfID":         i.EgressVrfID,
		"IPTos":               i.IPTos,
		"ForwardingStatus":    i.ForwardingStatus,
		"IPTTL":               i.IPTTL,
		"TCPFlags":            i.TCPFlags,
		"IcmpType":            i.IcmpType,
		"IcmpCode":            i.IcmpCode,
		"IPv6FlowLabel":       i.IPv6FlowLabel,
		"FragmentId":          i.FragmentId,
		"FragmentOffset":      i.FragmentOffset,
		"BiFlowDirection":     i.BiFlowDirection,
		"SrcAS":               i.SrcAS,
		"DstAS":               i.DstAS,
		"NextHop":             i.NextHop,
		"NextHopAS":           i.NextHopAS,
		"SrcNet":              i.SrcNet,
		"DstNet":              i.DstNet,
		"EtypeName":           i.EtypeName,
		"ProtoName":           i.ProtoName,
		"IcmpName":            i.IcmpName,	
	}, bigquery.NoDedupeID, nil
}

func (d *BigQueryDriver) Send(key, data []byte) error {
	inserter := d.client.Dataset(d.bigQueryDatasetID).Table(d.bigQueryTableID).Inserter()
	var items Item
	json.Unmarshal(data, &items)
	if err := inserter.Put(context.Background(), items); err != nil {
		return err
	}
	return nil
}

func (d *BigQueryDriver) Close(context.Context) error {
	defer d.client.Close()
	return nil
}

func init() {
	d := &BigQueryDriver{}
	transport.RegisterTransportDriver("bigquery", d)
}
