package utils

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestMissingFlowsTracker_countMissingFlows(t *testing.T) {
	tests := []struct {
		name                    string
		sequenceTrackerKey      string
		seqnum                  uint32
		flowCount               uint16
		savedSeqTracker         map[string]int64
		expectedMissingFlows    int64
		expectedSeqReset        int
		expectedSavedSeqTracker map[string]int64
	}{
		{
			name:                 "no saved seq tracker yet",
			savedSeqTracker:      map[string]int64{},
			sequenceTrackerKey:   "127.0.01",
			seqnum:               100,
			flowCount:            100,
			expectedMissingFlows: 0,
			expectedSavedSeqTracker: map[string]int64{
				"127.0.01": 100,
			},
		},
		{
			name: "no missing flows",
			savedSeqTracker: map[string]int64{
				"127.0.01": 100,
			},
			sequenceTrackerKey:   "127.0.01",
			seqnum:               200,
			flowCount:            100,
			expectedMissingFlows: 0,
			expectedSavedSeqTracker: map[string]int64{
				"127.0.01": 200,
			},
		},
		{
			name: "have missing flows",
			savedSeqTracker: map[string]int64{
				"127.0.01": 100,
			},
			sequenceTrackerKey:   "127.0.01",
			seqnum:               200,
			flowCount:            30,
			expectedMissingFlows: 70,
			expectedSavedSeqTracker: map[string]int64{
				"127.0.01": 130,
			},
		},
		{
			name: "negative saved sequence tracker",
			// reported missing flows count can be temporarily negative when udp packet arrive unordered,
			// when slightly lower sequence number arrives after higher sequence number.
			savedSeqTracker: map[string]int64{
				"127.0.01": 1000,
			},
			sequenceTrackerKey:   "127.0.01",
			seqnum:               950,
			flowCount:            10,
			expectedMissingFlows: -60,
			expectedSavedSeqTracker: map[string]int64{
				"127.0.01": 1010,
			},
		},
		{
			name: "sequence number reset",
			savedSeqTracker: map[string]int64{
				"127.0.01": 9000,
			},
			sequenceTrackerKey:   "127.0.01",
			seqnum:               2000,
			flowCount:            100,
			expectedMissingFlows: 0,
			expectedSeqReset:     1,
			expectedSavedSeqTracker: map[string]int64{
				"127.0.01": 2000,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewMissingFlowsTracker(1000)
			s.counters = tt.savedSeqTracker
			missingFlows, seqReset := s.countMissing(tt.sequenceTrackerKey, tt.seqnum, tt.flowCount)
			assert.Equal(t, tt.expectedMissingFlows, missingFlows)
			assert.Equal(t, tt.expectedSeqReset, seqReset)
			assert.Equal(t, tt.expectedSavedSeqTracker, s.counters)
		})
	}
}

func TestMissingFlowsTracker_unorderedPackets(t *testing.T) {
	tracker := NewMissingFlowsTracker(1000)
	key := "1.2.3.4|0|1" // a NetFlow5 key `samplerIp|engineType|engineId`

	// We send 6 packets
	//
	// Packets sent by device:
	// P1: seq=10, flows=10
	// P2: seq=20, flows=10
	// P3: seq=30, flows=10
	// P4: seq=40, flows=10
	// P5: seq=50, flows=10
	// P6: seq=60, flows=10
	//
	// Packets Received by goflow2:
	// P1: seq=10, flows=10
	//                      // P2 has been lost
	// P3: seq=30, flows=10
	// P5: seq=50, flows=10 // P5 received before P4
	// P4: seq=40, flows=10
	// P6: seq=60, flows=10
	//
	// We expected to see 10 flows dropped at the end.

	// P1
	missing, reset := tracker.countMissing(key, 10, 10)
	assert.Equal(t, int64(0), missing)
	assert.Equal(t, 0, reset)

	// P3
	missing, reset = tracker.countMissing(key, 30, 10)
	assert.Equal(t, int64(10), missing)
	assert.Equal(t, 0, reset)

	// P5
	missing, reset = tracker.countMissing(key, 50, 10)
	assert.Equal(t, int64(20), missing)
	assert.Equal(t, 0, reset)

	// P4
	missing, reset = tracker.countMissing(key, 40, 10)
	assert.Equal(t, int64(0), missing)
	assert.Equal(t, 0, reset)

	// P6
	missing, reset = tracker.countMissing(key, 60, 10)
	assert.Equal(t, int64(10), missing)
	assert.Equal(t, 0, reset)
}
