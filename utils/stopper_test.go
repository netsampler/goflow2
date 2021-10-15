package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStopper(t *testing.T) {
	r := routine{}
	require.False(t, r.Running)
	require.NoError(t, r.StartRoutine())
	assert.True(t, r.Running)
	r.Shutdown()
	assert.Eventually(t, func() bool {
		return r.Running == false
	}, time.Second, time.Millisecond)

	// after shutdown, we can start it again
	require.NoError(t, r.StartRoutine())
	assert.True(t, r.Running)
}

func TestStopper_CannotStartTwice(t *testing.T) {
	r := routine{}
	require.False(t, r.Running)
	require.NoError(t, r.StartRoutine())
	assert.ErrorIs(t, r.StartRoutine(), ErrAlreadyStarted)
}

type routine struct {
	stopper
	Running bool
}

func (p *routine) StartRoutine() error {
	if err := p.start(); err != nil {
		return err
	}
	p.Running = true
	waitForGoRoutine := make(chan struct{})
	go func() {
		close(waitForGoRoutine)
		<-p.stopCh
		p.Running = false
	}()
	<-waitForGoRoutine
	return nil
}
