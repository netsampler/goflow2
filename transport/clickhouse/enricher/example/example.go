package example

import (
	"context"
	"sync"

	"github.com/netsampler/goflow2/v2/transport/clickhouse/common"
)

const (
	name          = "example"
	ColDstService = "dst_service"
)

var (
	defaultExampleEnricher *exampleEnricher
	once                   sync.Once
)

type exampleEnricher struct{}

var _ common.Enricher = &exampleEnricher{}

func new() (common.Enricher, error) {
	once.Do(func() {
		defaultExampleEnricher = &exampleEnricher{}
	})
	return defaultExampleEnricher, nil
}

func (m *exampleEnricher) Proceed(record common.EnrichmentRecord) error {
	if dstPort, ok := record.Get(common.FlowDstPort).(uint32); ok {
		switch dstPort {
		case 22:
			record.Set(ColDstService, "ssh")
		case 80:
			record.Set(ColDstService, "http")
		case 443:
			record.Set(ColDstService, "https")
		default:
			record.Set(ColDstService, "unknown")
		}
	}
	return nil
}

func (m *exampleEnricher) Name() string {
	return name
}

func (m *exampleEnricher) Init(ctx context.Context) error {
	return nil
}

func init() {
	common.RegisterEnricherFabricFunc(name, new)
}
