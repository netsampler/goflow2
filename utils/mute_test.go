package utils

import (
	"testing"
	"time"
)

func TestBatchMute(t *testing.T) {
	tm := time.Date(2023, time.November, 10, 23, 0, 0, 0, time.UTC)
	bm := BatchMute{
		batchTime:     tm,
		resetInterval: time.Second * 10,
		max:           5,
	}

	for i := 0; i < 20; i++ {
		tm = tm.Add(time.Second)
		t.Log(bm.increment(1, tm))
	}

}

func TestBatchMuteZero(t *testing.T) {
	tm := time.Date(2023, time.November, 10, 23, 0, 0, 0, time.UTC)
	bm := BatchMute{
		batchTime:     tm,
		resetInterval: time.Second * 10,
		max:           0,
	}

	for i := 0; i < 20; i++ {
		tm = tm.Add(time.Second)
		t.Log(bm.increment(1, tm))
	}

}

func TestBatchMuteInterval(t *testing.T) {
	tm := time.Date(2023, time.November, 10, 23, 0, 0, 0, time.UTC)
	bm := BatchMute{
		batchTime:     tm,
		resetInterval: 0,
		max:           5,
	}

	for i := 0; i < 20; i++ {
		tm = tm.Add(time.Second)
		t.Log(bm.increment(1, tm))
	}

}
