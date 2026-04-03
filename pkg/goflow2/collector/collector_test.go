package collector

import (
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/goflow2/listen"
	"github.com/netsampler/goflow2/v3/transport"
)

type testTransportDriver struct {
	errCh chan error
}

func (d *testTransportDriver) Prepare() error {
	return nil
}

func (d *testTransportDriver) Init() error {
	return nil
}

func (d *testTransportDriver) Close() error {
	return nil
}

func (d *testTransportDriver) Send(key, data []byte) error {
	return nil
}

func (d *testTransportDriver) Errors() <-chan error {
	return d.errCh
}

func TestCollectorStopAfterTransportErrorsClose(t *testing.T) {
	driver := &testTransportDriver{errCh: make(chan error)}
	transportName := fmt.Sprintf("test-transport-%d", time.Now().UnixNano())
	transport.RegisterTransportDriver(transportName, driver)
	transportObj, err := transport.FindTransport(transportName)
	if err != nil {
		t.Fatalf("find transport: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	coll, err := New(Config{
		Listeners:     []listen.ListenerConfig{},
		Formatter:     nil,
		Transport:     transportObj,
		Producer:      nil,
		TemplateStore: nil,
		ErrCnt:        1,
		ErrInt:        time.Millisecond,
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("new collector: %v", err)
	}

	if err := coll.Start(); err != nil {
		t.Fatalf("start collector: %v", err)
	}

	close(driver.errCh)

	done := make(chan struct{})
	go func() {
		coll.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for collector stop")
	}
}
