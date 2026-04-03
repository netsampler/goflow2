package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/utils/store/samplingrate"
	"github.com/netsampler/goflow2/v3/utils/store/templates"
)

// Config groups flowstore JSON persistence settings.
type Config struct {
	Path     string
	Interval time.Duration
}

type filePersistence struct {
	path       string
	interval   time.Duration
	changeCh   chan struct{}
	marshal    func() ([]byte, error)
	stopCh     chan struct{}
	doneCh     chan struct{}
	startOnce  sync.Once
	closeOnce  sync.Once
	flushMutex sync.Mutex
	emitError  func(error)
}

// Manager owns JSON persistence for sampling-rate and template flowstores.
type Manager struct {
	errCh     chan error
	errMu     sync.Mutex
	errClosed bool
	closeOnce sync.Once
	stateMu   sync.Mutex

	file          *filePersistence
	samplingStore samplingrate.Store
	templateStore netflow.ManagedTemplateStore
	samplingOpen  bool
	templateOpen  bool
	preloadDoc    map[string]json.RawMessage
	preloadErr    error
	preloadOnce   sync.Once
}

const (
	documentTemplatesKey   = "templates"
	documentSampleRatesKey = "sampling-rates"
)

// New creates a new persistence manager.
func New(cfg Config) *Manager {
	return &Manager{
		errCh: make(chan error, 16),
		file: &filePersistence{
			path:      cfg.Path,
			interval:  cfg.Interval,
			changeCh:  make(chan struct{}, 1),
			marshal:   nil,
			emitError: nil,
		},
	}
}

// Errors exposes asynchronous persistence errors.
func (m *Manager) Errors() <-chan error {
	return m.errCh
}

// NewSamplingRateStore creates and preloads a sampling-rate store with JSON hooks.
func (m *Manager) NewSamplingRateStore(opts ...samplingrate.FlowStoreOption) (samplingrate.Store, error) {
	if m == nil {
		return samplingrate.NewSamplingRateFlowStore(opts...), nil
	}

	file := m.ensureFilePersistence()
	storeOpts := append([]samplingrate.FlowStoreOption{}, opts...)
	storeOpts = append(storeOpts, samplingrate.WithHooks(samplingrate.PersistenceHooks(file.notifyChange)))
	storeOpts = append(storeOpts, samplingrate.WithCloseHook(m.newStoreCloseHook(documentSampleRatesKey)))
	store := samplingrate.NewSamplingRateFlowStore(storeOpts...)

	if err := m.preload(documentSampleRatesKey, func(buf []byte) error {
		return samplingrate.LoadJSON(store, buf)
	}); err != nil {
		return nil, err
	}

	m.samplingStore = store
	m.samplingOpen = true
	m.file = file
	return store, nil
}

// NewTemplateStore creates and preloads a template store with JSON hooks.
func (m *Manager) NewTemplateStore(opts ...templates.FlowStoreOption) (netflow.ManagedTemplateStore, error) {
	if m == nil {
		return templates.NewTemplateFlowStore(opts...), nil
	}

	file := m.ensureFilePersistence()
	storeOpts := append([]templates.FlowStoreOption{}, opts...)
	storeOpts = append(storeOpts, templates.WithHooks(templates.PersistenceHooks(file.notifyChange)))
	storeOpts = append(storeOpts, templates.WithCloseHook(m.newStoreCloseHook(documentTemplatesKey)))
	store := templates.NewTemplateFlowStore(storeOpts...)

	if err := m.preload(documentTemplatesKey, func(buf []byte) error {
		return templates.LoadJSON(store, buf)
	}); err != nil {
		return nil, err
	}

	m.templateStore = store
	m.templateOpen = true
	m.file = file
	return store, nil
}

// Start starts file flush loops for configured persistence layers.
func (m *Manager) Start() {
	if m == nil {
		return
	}
	if m.file != nil {
		m.file.start()
	}
}

// Close flushes pending state, closes the shared persistence file, and closes the error channel.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		if m.file != nil {
			m.file.close()
		}
		m.errMu.Lock()
		if !m.errClosed {
			close(m.errCh)
			m.errClosed = true
		}
		m.errMu.Unlock()
	})
}

// newStoreCloseHook adapts a document section name into a store close callback.
func (m *Manager) newStoreCloseHook(section string) func() {
	return func() {
		m.closeStore(section)
	}
}

// closeStore marks one logical store closed, forces a final flush, and closes
// the shared file persistence once the last managed store has shut down.
func (m *Manager) closeStore(section string) {
	if m == nil {
		return
	}

	var file *filePersistence
	var closeFile bool

	m.stateMu.Lock()
	switch section {
	case documentSampleRatesKey:
		if !m.samplingOpen {
			m.stateMu.Unlock()
			return
		}
		m.samplingOpen = false
	case documentTemplatesKey:
		if !m.templateOpen {
			m.stateMu.Unlock()
			return
		}
		m.templateOpen = false
	default:
		m.stateMu.Unlock()
		return
	}
	file = m.file
	closeFile = !m.samplingOpen && !m.templateOpen
	m.stateMu.Unlock()

	if file == nil {
		return
	}
	file.flush()
	if closeFile {
		file.close()
	}
}

// ensureFilePersistence initializes the shared file persistence wiring lazily.
func (m *Manager) ensureFilePersistence() *filePersistence {
	if m.file != nil {
		if m.file.marshal == nil {
			m.file.marshal = m.marshalDocument
		}
		if m.file.emitError == nil {
			m.file.emitError = func(err error) { m.emitError(fmt.Errorf("store persistence: %w", err)) }
		}
		return m.file
	}
	m.file = &filePersistence{
		changeCh:  make(chan struct{}, 1),
		marshal:   m.marshalDocument,
		emitError: func(err error) { m.emitError(fmt.Errorf("store persistence: %w", err)) },
	}
	return m.file
}

// preload loads one JSON document section into a target store before runtime use.
func (m *Manager) preload(section string, load func([]byte) error) error {
	if m == nil || load == nil {
		return nil
	}
	document, err := m.loadPreloadDocument()
	if err != nil {
		return err
	}
	if len(document) == 0 {
		return nil
	}
	sectionData, ok := document[section]
	if !ok || len(sectionData) == 0 || string(sectionData) == "null" {
		return nil
	}
	if err := load(sectionData); err != nil {
		return fmt.Errorf("load %s %s: %w", m.file.path, section, err)
	}
	return nil
}

// loadPreloadDocument reads and caches the persistence document used for startup preload.
func (m *Manager) loadPreloadDocument() (map[string]json.RawMessage, error) {
	if m == nil || m.file == nil || m.file.path == "" {
		return nil, nil
	}
	m.preloadOnce.Do(func() {
		data, err := os.ReadFile(m.file.path)
		if err != nil {
			if os.IsNotExist(err) {
				m.preloadDoc = map[string]json.RawMessage{}
				return
			}
			m.preloadErr = fmt.Errorf("read %s: %w", m.file.path, err)
			return
		}
		if len(data) == 0 {
			m.preloadDoc = map[string]json.RawMessage{}
			return
		}

		var document map[string]json.RawMessage
		if err := json.Unmarshal(data, &document); err != nil {
			m.preloadErr = fmt.Errorf("decode %s: %w", m.file.path, err)
			return
		}
		m.preloadDoc = document
	})
	if m.preloadErr != nil {
		return nil, m.preloadErr
	}
	return m.preloadDoc, nil
}

// marshalDocument renders the combined template and sampling-rate snapshot into one JSON document.
func (m *Manager) marshalDocument() ([]byte, error) {
	document := make(map[string]json.RawMessage, 2)
	if m.templateStore != nil {
		data, err := templates.MarshalJSONSnapshot(m.templateStore)
		if err != nil {
			return nil, fmt.Errorf("marshal templates: %w", err)
		}
		document[documentTemplatesKey] = data
	}
	if m.samplingStore != nil {
		data, err := samplingrate.MarshalJSONSnapshot(m.samplingStore)
		if err != nil {
			return nil, fmt.Errorf("marshal sampling-rates: %w", err)
		}
		document[documentSampleRatesKey] = data
	}
	return json.Marshal(document)
}

// Document returns the combined flowstore JSON document for HTTP rendering.
func (m *Manager) Document() []byte {
	if m == nil {
		return nil
	}
	data, err := m.marshalDocument()
	if err != nil {
		m.emitError(err)
		return nil
	}
	return data
}

// emitError best-effort publishes asynchronous persistence errors without blocking callers.
func (m *Manager) emitError(err error) {
	if err == nil {
		return
	}
	m.errMu.Lock()
	defer m.errMu.Unlock()
	if m.errClosed {
		return
	}
	select {
	case m.errCh <- err:
	default:
	}
}

// notifyChange coalesces change notifications for the background file writer.
func (p *filePersistence) notifyChange() {
	if p == nil || p.path == "" {
		return
	}
	select {
	case p.changeCh <- struct{}{}:
	default:
	}
}

// start launches the optional delayed flush loop for file persistence.
func (p *filePersistence) start() {
	if p == nil || p.path == "" || p.marshal == nil {
		return
	}
	p.startOnce.Do(func() {
		p.stopCh = make(chan struct{})
		p.doneCh = make(chan struct{})

		go func() {
			var timer *time.Timer
			var timerCh <-chan time.Time
			defer func() {
				if timer != nil {
					timer.Stop()
				}
				close(p.doneCh)
			}()

			for {
				select {
				case <-p.stopCh:
					return
				case <-p.changeCh:
					if p.interval <= 0 {
						p.flush()
						continue
					}
					if timer == nil {
						timer = time.NewTimer(p.interval)
						timerCh = timer.C
					}
				case <-timerCh:
					if timer != nil {
						timer.Stop()
						timer = nil
						timerCh = nil
					}
					p.flush()
				}
			}
		}()
	})
}

// close stops the flush loop and performs one last synchronous write.
func (p *filePersistence) close() {
	if p == nil {
		return
	}
	p.closeOnce.Do(func() {
		if p.stopCh != nil {
			close(p.stopCh)
			<-p.doneCh
		}
		p.flush()
	})
}

// flush serializes the current document and atomically writes it to disk.
func (p *filePersistence) flush() {
	if p == nil || p.path == "" || p.marshal == nil {
		return
	}
	p.flushMutex.Lock()
	defer p.flushMutex.Unlock()

	data, err := p.marshal()
	if err != nil {
		p.emitError(err)
		return
	}
	if err := writeAtomic(p.path, data, 0o644); err != nil {
		p.emitError(err)
	}
}

// writeAtomic writes data to a temp file and renames it into place.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	tmpPath := path + "_tmp"
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("open temp %s: %w", tmpPath, err)
	}
	if _, err := tmpFile.Write(data); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			return fmt.Errorf("close temp after write %s: %w", tmpPath, closeErr)
		}
		return fmt.Errorf("write temp %s: %w", tmpPath, err)
	}
	if err := tmpFile.Sync(); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			return fmt.Errorf("close temp after sync %s: %w", tmpPath, closeErr)
		}
		return fmt.Errorf("sync temp %s: %w", tmpPath, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpPath, path, err)
	}
	return nil
}
