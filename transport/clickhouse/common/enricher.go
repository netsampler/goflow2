package common

import (
	"context"
	"sync"
)

var (
	enricherFabric  map[string]func() (Enricher, error) = make(map[string]func() (Enricher, error))
	enricherPlugins map[string]Enricher                 = make(map[string]Enricher)
	lock            sync.Mutex
)

type Enricher interface {
	Proceed(record EnrichmentRecord) error
	Init(ctx context.Context) error
	Name() string
}

func RegisterEnricherFabricFunc(name string, fabricFunction func() (Enricher, error)) {
	lock.Lock()
	defer lock.Unlock()
	enricherFabric[name] = fabricFunction
}

func Enrichers(enrichers []string) ([]Enricher, error) {
	lock.Lock()
	defer lock.Unlock()
	response := []Enricher{}

	enabledEnrichers := make(map[string]struct{}, len(enrichers))
	for _, m := range enrichers {
		enabledEnrichers[m] = struct{}{}
	}

	for name, fabricFunc := range enricherFabric {
		if _, ok := enabledEnrichers[name]; ok {
			e, err := fabricFunc()
			if err != nil {
				return nil, err
			}
			enricherPlugins[e.Name()] = e
			response = append(response, e)
		}
	}
	return response, nil
}
