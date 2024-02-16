package state

import (
	"flag"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/netsampler/goflow2/v2/producer/proto"
)

var StateSampling = flag.String("state.sampling", "memory://", fmt.Sprintf("Define state sampling rate engine URL (available schemes: %s)", strings.Join(SupportedSchemes, ", ")))
var samplingRateDB State[samplingRateKey, uint32]
var samplingRateInitLock = new(sync.Mutex)

type samplingRateKey struct {
	Key         string `json:"key"`
	Version     uint16 `json:"ver"`
	ObsDomainId uint32 `json:"obs"`
}

type SamplingRateSystem struct {
	key string
}

func (s *SamplingRateSystem) GetSamplingRate(version uint16, obsDomainId uint32) (uint32, error) {
	return samplingRateDB.Get(samplingRateKey{
		Key:         s.key,
		Version:     version,
		ObsDomainId: obsDomainId,
	})
}

func (s *SamplingRateSystem) AddSamplingRate(version uint16, obsDomainId uint32, samplingRate uint32) {
	_ = samplingRateDB.Add(samplingRateKey{
		Key:         s.key,
		Version:     version,
		ObsDomainId: obsDomainId,
	}, samplingRate)
}

func CreateSamplingSystem(key string) protoproducer.SamplingRateSystem {
	ts := &SamplingRateSystem{
		key: key,
	}
	return ts
}

func InitSamplingRate() error {
	samplingRateInitLock.Lock()
	defer samplingRateInitLock.Unlock()
	if samplingRateDB != nil {
		return nil
	}
	samplingUrl, err := url.Parse(*StateSampling)
	if err != nil {
		return err
	}
	if !samplingUrl.Query().Has("prefix") {
		q := samplingUrl.Query()
		q.Set("prefix", "goflow2:sampling_rate:")
		samplingUrl.RawQuery = q.Encode()
	}
	samplingRateDB, err = NewState[samplingRateKey, uint32](samplingUrl.String())
	return err
}

func CloseSamplingRate() error {
	samplingRateInitLock.Lock()
	defer samplingRateInitLock.Unlock()
	if samplingRateDB == nil {
		return nil
	}
	err := samplingRateDB.Close()
	samplingRateDB = nil
	return err
}
