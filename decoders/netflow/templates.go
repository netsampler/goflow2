package netflow

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"

	"github.com/netsampler/goflow2/v2/state"
)

var (
	ErrorTemplateNotFound = fmt.Errorf("Error template not found")
	StateTemplates        = flag.String("state.netflow.templates", "memory://", fmt.Sprintf("Define state templates engine URL (available schemes: %s)", strings.Join(state.SupportedSchemes, ", ")))
	templatesDB           state.State[templatesKey, templatesValue]
	templatesInitLock     = new(sync.Mutex)
)

// Store interface that allows storing, removing and retrieving template data
type NetFlowTemplateSystem interface {
	RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error)
	GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error)
	AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) error
}

type templatesKey struct {
	Key         string `json:"key"`
	Version     uint16 `json:"ver"`
	ObsDomainId uint32 `json:"obs"`
	TemplateID  uint16 `json:"tid"`
}

const (
	templateTypeTest                       = 0
	templateTypeTemplateRecord             = 1
	templateTypeIPFIXOptionsTemplateRecord = 2
	templateTypeNFv9OptionsTemplateRecord  = 3
)

type templatesValue struct {
	TemplateType int         `json:"ttype"`
	Data         interface{} `json:"data"`
}

type templatesValueUnmarshal struct {
	TemplateType int             `json:"ttype"`
	Data         json.RawMessage `json:"data"`
}

func (t *templatesValue) UnmarshalJSON(bytes []byte) error {
	var v templatesValueUnmarshal
	err := json.Unmarshal(bytes, &v)
	if err != nil {
		return err
	}
	t.TemplateType = v.TemplateType
	switch v.TemplateType {
	case templateTypeTest:
		var data int
		err = json.Unmarshal(v.Data, &data)
		if err != nil {
			return err
		}
		t.Data = data
	case templateTypeTemplateRecord:
		var data TemplateRecord
		err = json.Unmarshal(v.Data, &data)
		if err != nil {
			return err
		}
		t.Data = data
	case templateTypeIPFIXOptionsTemplateRecord:
		var data IPFIXOptionsTemplateRecord
		err = json.Unmarshal(v.Data, &data)
		if err != nil {
			return err
		}
		t.Data = data
	case templateTypeNFv9OptionsTemplateRecord:
		var data NFv9OptionsTemplateRecord
		err = json.Unmarshal(v.Data, &data)
		if err != nil {
			return err
		}
		t.Data = data
	default:
		return fmt.Errorf("unknown template type: %d", v.TemplateType)
	}
	return nil
}

type NetflowTemplate struct {
	key string
}

func (t *NetflowTemplate) RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	if v, err := templatesDB.Pop(templatesKey{
		Key:         t.key,
		Version:     version,
		ObsDomainId: obsDomainId,
		TemplateID:  templateId,
	}); err != nil && errors.Is(err, state.ErrorKeyNotFound) {
		return nil, ErrorTemplateNotFound
	} else if err != nil {
		return nil, err
	} else {
		return v.Data, nil
	}
}

func (t *NetflowTemplate) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	if v, err := templatesDB.Get(templatesKey{
		Key:         t.key,
		Version:     version,
		ObsDomainId: obsDomainId,
		TemplateID:  templateId,
	}); err != nil && errors.Is(err, state.ErrorKeyNotFound) {
		return nil, ErrorTemplateNotFound
	} else if err != nil {
		return nil, err
	} else {
		return v.Data, nil
	}
}

func (t *NetflowTemplate) AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) error {
	k := templatesKey{
		Key:         t.key,
		Version:     version,
		ObsDomainId: obsDomainId,
		TemplateID:  templateId,
	}
	var err error
	switch templatec := template.(type) {
	case TemplateRecord:
		err = templatesDB.Add(k, templatesValue{
			TemplateType: templateTypeTemplateRecord,
			Data:         templatec,
		})
	case IPFIXOptionsTemplateRecord:
		err = templatesDB.Add(k, templatesValue{
			TemplateType: templateTypeIPFIXOptionsTemplateRecord,
			Data:         templatec,
		})
	case NFv9OptionsTemplateRecord:
		err = templatesDB.Add(k, templatesValue{
			TemplateType: templateTypeNFv9OptionsTemplateRecord,
			Data:         templatec,
		})
	case int:
		err = templatesDB.Add(k, templatesValue{
			TemplateType: templateTypeTest,
			Data:         templatec,
		})
	default:
		return fmt.Errorf("unknown template type: %s", reflect.TypeOf(template).String())
	}
	return err
}

func CreateTemplateSystem(key string) NetFlowTemplateSystem {
	ts := &NetflowTemplate{
		key: key,
	}
	return ts
}

func InitTemplates() error {
	templatesInitLock.Lock()
	defer templatesInitLock.Unlock()
	if templatesDB != nil {
		return nil
	}
	templatesUrl, err := url.Parse(*StateTemplates)
	if err != nil {
		return err
	}
	if !templatesUrl.Query().Has("prefix") {
		q := templatesUrl.Query()
		q.Set("prefix", "goflow2:nf_templates:")
		templatesUrl.RawQuery = q.Encode()
	}
	templatesDB, err = state.NewState[templatesKey, templatesValue](templatesUrl.String())
	return err
}

func CloseTemplates() error {
	templatesInitLock.Lock()
	defer templatesInitLock.Unlock()
	if templatesDB == nil {
		return nil
	}
	err := templatesDB.Close()
	templatesDB = nil
	return err
}
