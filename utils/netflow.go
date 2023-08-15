package utils

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/netsampler/goflow2/decoders/netflow"
	"github.com/netsampler/goflow2/decoders/netflow/templates"
	"github.com/netsampler/goflow2/format"
	flowmessage "github.com/netsampler/goflow2/pb"
	"github.com/netsampler/goflow2/producer"
	"github.com/netsampler/goflow2/transport"
	"github.com/prometheus/client_golang/prometheus"
)

/*
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
	NetFlowTemplatesStats.With(
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
*/

type StateNetFlow struct {
	stopper

	Format    format.FormatInterface
	Transport transport.TransportInterface
	Logger    Logger
	/*templateslock *sync.RWMutex
	templates     map[string]*TemplateSystem*/

	samplinglock *sync.RWMutex
	sampling     map[string]producer.SamplingRateSystem

	Config       *producer.ProducerConfig
	configMapped *producer.ProducerConfigMapped

	TemplateSystem templates.TemplateInterface

	ctx context.Context
}

func NewStateNetFlow() *StateNetFlow {
	return &StateNetFlow{
		ctx:          context.Background(),
		samplinglock: &sync.RWMutex{},
		sampling:     make(map[string]producer.SamplingRateSystem),
	}
}

func (s *StateNetFlow) DecodeFlow(msg interface{}) error {
	pkt := msg.(BaseMessage)
	buf := bytes.NewBuffer(pkt.Payload)

	key := pkt.Src.String()
	samplerAddress := pkt.Src
	if samplerAddress.To4() != nil {
		samplerAddress = samplerAddress.To4()
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

	ts := uint64(time.Now().UTC().Unix())
	if pkt.SetTime {
		ts = uint64(pkt.RecvTime.UTC().Unix())
	}

	timeTrackStart := time.Now()
	msgDec, err := netflow.DecodeMessageContext(s.ctx, buf, key, netflow.TemplateWrapper{s.ctx, key, s.TemplateSystem})
	if err != nil {
		switch err.(type) {
		case *netflow.ErrorTemplateNotFound:
			NetFlowErrors.With(
				prometheus.Labels{
					"router": key,
					"error":  "template_not_found",
				}).
				Inc()
		default:
			NetFlowErrors.With(
				prometheus.Labels{
					"router": key,
					"error":  "error_decoding",
				}).
				Inc()
		}
		return err
	}

	var flowMessageSet []*flowmessage.FlowMessage

	switch msgDecConv := msgDec.(type) {
	case netflow.NFv9Packet:
		NetFlowStats.With(
			prometheus.Labels{
				"router":  key,
				"version": "9",
			}).
			Inc()

		for _, fs := range msgDecConv.FlowSets {
			switch fsConv := fs.(type) {
			case netflow.TemplateFlowSet:
				NetFlowSetStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "9",
						"type":    "TemplateFlowSet",
					}).
					Inc()

				NetFlowSetRecordsStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "9",
						"type":    "TemplateFlowSet",
					}).
					Add(float64(len(fsConv.Records)))

			case netflow.NFv9OptionsTemplateFlowSet:
				NetFlowSetStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "9",
						"type":    "OptionsTemplateFlowSet",
					}).
					Inc()

				NetFlowSetRecordsStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "9",
						"type":    "OptionsTemplateFlowSet",
					}).
					Add(float64(len(fsConv.Records)))

			case netflow.OptionsDataFlowSet:
				NetFlowSetStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "9",
						"type":    "OptionsDataFlowSet",
					}).
					Inc()

				NetFlowSetRecordsStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "9",
						"type":    "OptionsDataFlowSet",
					}).
					Add(float64(len(fsConv.Records)))
			case netflow.DataFlowSet:
				NetFlowSetStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "9",
						"type":    "DataFlowSet",
					}).
					Inc()

				NetFlowSetRecordsStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "9",
						"type":    "DataFlowSet",
					}).
					Add(float64(len(fsConv.Records)))
			}
		}
		flowMessageSet, err = producer.ProcessMessageNetFlowConfig(msgDecConv, sampling, s.configMapped)

		for _, fmsg := range flowMessageSet {
			fmsg.TimeReceived = ts
			fmsg.SamplerAddress = samplerAddress
			timeDiff := fmsg.TimeReceived - fmsg.TimeFlowEnd
			NetFlowTimeStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"version": "9",
				}).
				Observe(float64(timeDiff))
		}
	case netflow.IPFIXPacket:
		NetFlowStats.With(
			prometheus.Labels{
				"router":  key,
				"version": "10",
			}).
			Inc()

		for _, fs := range msgDecConv.FlowSets {
			switch fsConv := fs.(type) {
			case netflow.TemplateFlowSet:
				NetFlowSetStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "10",
						"type":    "TemplateFlowSet",
					}).
					Inc()

				NetFlowSetRecordsStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "10",
						"type":    "TemplateFlowSet",
					}).
					Add(float64(len(fsConv.Records)))

			case netflow.IPFIXOptionsTemplateFlowSet:
				NetFlowSetStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "10",
						"type":    "OptionsTemplateFlowSet",
					}).
					Inc()

				NetFlowSetRecordsStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "10",
						"type":    "OptionsTemplateFlowSet",
					}).
					Add(float64(len(fsConv.Records)))

			case netflow.OptionsDataFlowSet:

				NetFlowSetStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "10",
						"type":    "OptionsDataFlowSet",
					}).
					Inc()

				NetFlowSetRecordsStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "10",
						"type":    "OptionsDataFlowSet",
					}).
					Add(float64(len(fsConv.Records)))

			case netflow.DataFlowSet:
				NetFlowSetStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "10",
						"type":    "DataFlowSet",
					}).
					Inc()

				NetFlowSetRecordsStatsSum.With(
					prometheus.Labels{
						"router":  key,
						"version": "10",
						"type":    "DataFlowSet",
					}).
					Add(float64(len(fsConv.Records)))
			}
		}
		flowMessageSet, err = producer.ProcessMessageNetFlowConfig(msgDecConv, sampling, s.configMapped)

		for _, fmsg := range flowMessageSet {
			fmsg.TimeReceived = ts
			fmsg.SamplerAddress = samplerAddress
			timeDiff := fmsg.TimeReceived - fmsg.TimeFlowEnd
			NetFlowTimeStatsSum.With(
				prometheus.Labels{
					"router":  key,
					"version": "10",
				}).
				Observe(float64(timeDiff))
		}
	}

	timeTrackStop := time.Now()
	DecoderTime.With(
		prometheus.Labels{
			"name": "NetFlow",
		}).
		Observe(float64((timeTrackStop.Sub(timeTrackStart)).Nanoseconds()) / 1000)

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

/*
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
}*/

func (s *StateNetFlow) initConfig() {
	s.configMapped = producer.NewProducerConfigMapped(s.Config)
}

func (s *StateNetFlow) FlowRoutine(workers int, addr string, port int, reuseport bool) error {
	if err := s.start(); err != nil {
		return err
	}
	//s.InitTemplates()
	s.initConfig()
	return UDPStoppableRoutine(s.stopCh, "NetFlow", s.DecodeFlow, workers, addr, port, reuseport, s.Logger)
}

// FlowRoutineCtx?
