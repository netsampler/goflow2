package utils

import (
	"sync"
)

// MissingFlowsTracker is used to track missing packets/flows
type MissingFlowsTracker struct {
	counters   map[string]int64 // counter key is based on source addr and sourceId/obsDomain/engineType/engineId
	countersMu *sync.RWMutex

	maxNegativeSequenceDifference int
}

func NewMissingFlowsTracker(maxNegativeSequenceDifference int) *MissingFlowsTracker {
	return &MissingFlowsTracker{
		counters:                      make(map[string]int64),
		countersMu:                    &sync.RWMutex{},
		maxNegativeSequenceDifference: maxNegativeSequenceDifference,
	}
}

func (s *MissingFlowsTracker) countMissing(key string, seqnum uint32, flows uint16) int64 {
	s.countersMu.Lock()
	defer s.countersMu.Unlock()

	if _, ok := s.counters[key]; !ok {
		s.counters[key] = int64(seqnum)
	} else {
		s.counters[key] += int64(flows)
	}
	missingElements := int64(seqnum) - s.counters[key]

	// We assume there is a sequence number reset when the number of missing flows/packets is negative and high.
	// When this happens, we reset the counter to the current sequence number.
	if missingElements <= -int64(s.maxNegativeSequenceDifference) {
		s.counters[key] = int64(seqnum)
		missingElements = 0
	}
	return missingElements
}
