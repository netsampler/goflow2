package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/netsampler/goflow2/decoders/netflow"
	"github.com/netsampler/goflow2/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/decoders/utils"
	"github.com/netsampler/goflow2/format"
	flowmessage "github.com/netsampler/goflow2/pb"
	"github.com/netsampler/goflow2/producer"
	"github.com/netsampler/goflow2/tmpmetrics" // temporary
	"github.com/netsampler/goflow2/transport"
)

type TemplateSystem struct {
	key       string
	templates *netflow.BasicTemplateSystem
}

func (s *TemplateSystem) AddTemplate(version uint16, obsDomainId uint32, template interface{}) {
	s.templates.AddTemplate(version, obsDomainId, template)

	typeStr := "options_template"
	var templateId uint16
	switch templateIdConv := template.(type) {
	case netflow.IPFIXOptionsTemplateRecord:
		templateId = templateIdConv.TemplateId
	case netflow.NFv9OptionsTemplateRecord:
		templateId = templateIdConv.TemplateId
	case netflow.TemplateRecord:
		templateId = templateIdConv.TemplateId
		typeStr = "template"
	}
	metrics.NetFlowTemplatesStats.With(
		prometheus.Labels{
			"router":        s.key,
			"version":       strconv.Itoa(int(version)),
			"obs_domain_id": strconv.Itoa(int(obsDomainId)),
			"template_id":   strconv.Itoa(int(templateId)),
			"type":          typeStr,
		}).
		Inc()
}

func (s *TemplateSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	return s.templates.GetTemplate(version, obsDomainId, templateId)
}

type StateNetFlow struct {
	stopper

	Format        format.FormatInterface
	Transport     transport.TransportInterface
	Logger        Logger
	templateslock *sync.RWMutex
	templates     map[string]*TemplateSystem

	samplinglock *sync.RWMutex
	sampling     map[string]producer.SamplingRateSystem

	Config       *producer.ProducerConfig
	configMapped *producer.ProducerConfigMapped
}

func (s *StateNetFlow) DecodeFlow(msg interface{}) error {
	pkt, ok := msg.(*Message)
	if !ok {
		return fmt.Errorf("flow is not *Message")
	}
	buf := bytes.NewBuffer(pkt.Payload)

	key := pkt.Src.String()
	addr := pkt.Src.Addr().As16()
	samplerAddress := addr[:]

	s.templateslock.RLock()
	templates, ok := s.templates[key]
	s.templateslock.RUnlock()
	if !ok {
		templates = &TemplateSystem{
			templates: netflow.CreateTemplateSystem(),
			key:       key,
		}
		s.templateslock.Lock()
		s.templates[key] = templates
		s.templateslock.Unlock()
	}
	s.samplinglock.RLock()
	sampling, ok := s.sampling[key]
	s.samplinglock.RUnlock()
	if !ok {
		sampling = producer.CreateSamplingSystem()
		s.samplinglock.Lock()
		s.sampling[key] = sampling
		s.samplinglock.Unlock()
	}

	ts := uint64(pkt.Received.Unix())

	var packetV5 netflowlegacy.PacketNetFlowV5
	var packetNFv9 netflow.NFv9Packet
	var packetIPFIX netflow.IPFIXPacket

	// decode the version
	var version uint16
	if err := utils.BinaryDecoder(buf, &version); err != nil {
		return err
	}
	switch version {
	case 5:
		if err := netflowlegacy.DecodeMessage(buf, &packetV5); err != nil {
			return err
		}
	case 9:
		if err := netflow.DecodeMessageNetFlow(buf, templates, &packetNFv9); err != nil {
			return err
		}
	case 10:
		if err := netflow.DecodeMessageIPFIX(buf, templates, &packetIPFIX); err != nil {
			return err
		}
	default:
		return fmt.Errorf("Not a NetFlow packet")
	}

	var flowMessageSet []*flowmessage.FlowMessage
	var err error

	args := producer.ProcessArgs{
		Config:             s.configMapped,
		SamplingRateSystem: sampling,
	}

	switch version {
	case 5:
		flowMessageSet, err = producer.ProcessMessage(packetV5, &args)
		if err != nil {
			return err
		}

	case 9:

		flowMessageSet, err = producer.ProcessMessage(&packetNFv9, &args)
		if err != nil {
			return err
		}

		for _, fmsg := range flowMessageSet {
			fmsg.TimeReceived = ts
			fmsg.SamplerAddress = samplerAddress
		}
	case 10:
		flowMessageSet, err = producer.ProcessMessage(&packetIPFIX, &args)
		if err != nil {
			return err
		}

		for _, fmsg := range flowMessageSet {
			fmsg.TimeReceived = ts
			fmsg.SamplerAddress = samplerAddress
		}
	}

	for _, fmsg := range flowMessageSet {
		if s.Format != nil {
			key, data, err := s.Format.Format(fmsg)

			if err != nil && s.Logger != nil {
				s.Logger.Error(err)
			}
			if err == nil && s.Transport != nil {
				err = s.Transport.Send(key, data)
				if err != nil {
					s.Logger.Error(err)
				}
			}
		}
	}

	return nil
}

func (s *StateNetFlow) ServeHTTPTemplates(w http.ResponseWriter, r *http.Request) {
	tmp := make(map[string]map[uint16]map[uint32]map[uint16]interface{})
	s.templateslock.RLock()
	for key, templatesrouterstr := range s.templates {
		templatesrouter := templatesrouterstr.templates.GetTemplates()
		tmp[key] = templatesrouter
	}
	s.templateslock.RUnlock()
	enc := json.NewEncoder(w)
	enc.Encode(tmp)
}

func (s *StateNetFlow) InitTemplates() {
	s.templates = make(map[string]*TemplateSystem)
	s.templateslock = &sync.RWMutex{}
	s.sampling = make(map[string]producer.SamplingRateSystem)
	s.samplinglock = &sync.RWMutex{}
}

func (s *StateNetFlow) initConfig() {
	s.configMapped = producer.NewProducerConfigMapped(s.Config)
}

func (s *StateNetFlow) FlowRoutine(workers int, addr string, port int, reuseport bool) error {
	if err := s.start(); err != nil {
		return err
	}
	s.InitTemplates()
	s.initConfig()
	return UDPStoppableRoutine(s.stopCh, "NetFlow", s.DecodeFlow, workers, addr, port, reuseport, s.Logger)
}
