package state

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

var StateTemplates = flag.String("state.netflow.templates", "memory://", fmt.Sprintf("Define state templates engine URL (available schemes: %s)", strings.Join(SupportedSchemes, ", ")))
var templatesDB State[templatesKey, templatesValue]

type templatesKey struct {
	Key         string `json:"key"`
	Version     uint16 `json:"ver"`
	ObsDomainId uint32 `json:"obs"`
	TemplateID  uint16 `json:"tid"`
}

const (
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
	case templateTypeTemplateRecord:
		var data netflow.TemplateRecord
		err = json.Unmarshal(v.Data, &data)
		if err != nil {
			return err
		}
		t.Data = data
	case templateTypeIPFIXOptionsTemplateRecord:
		var data netflow.IPFIXOptionsTemplateRecord
		err = json.Unmarshal(v.Data, &data)
		if err != nil {
			return err
		}
		t.Data = data
	case templateTypeNFv9OptionsTemplateRecord:
		var data netflow.NFv9OptionsTemplateRecord
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

type TemplateSystem struct {
	key string
}

func (t *TemplateSystem) RemoveTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	if v, err := templatesDB.Pop(templatesKey{
		Key:         t.key,
		Version:     version,
		ObsDomainId: obsDomainId,
		TemplateID:  templateId,
	}); err != nil && errors.Is(err, ErrorKeyNotFound) {
		return nil, netflow.ErrorTemplateNotFound
	} else if err != nil {
		return nil, err
	} else {
		return v.Data, nil
	}
}

func (t *TemplateSystem) GetTemplate(version uint16, obsDomainId uint32, templateId uint16) (interface{}, error) {
	if v, err := templatesDB.Get(templatesKey{
		Key:         t.key,
		Version:     version,
		ObsDomainId: obsDomainId,
		TemplateID:  templateId,
	}); err != nil && errors.Is(err, ErrorKeyNotFound) {
		return nil, netflow.ErrorTemplateNotFound
	} else if err != nil {
		return nil, err
	} else {
		return v.Data, nil
	}
}

func (t *TemplateSystem) AddTemplate(version uint16, obsDomainId uint32, templateId uint16, template interface{}) error {
	k := templatesKey{
		Key:         t.key,
		Version:     version,
		ObsDomainId: obsDomainId,
		TemplateID:  templateId,
	}
	var err error
	switch templatec := template.(type) {
	case netflow.TemplateRecord:
		err = templatesDB.Add(k, templatesValue{
			TemplateType: templateTypeTemplateRecord,
			Data:         templatec,
		})
	case netflow.IPFIXOptionsTemplateRecord:
		err = templatesDB.Add(k, templatesValue{
			TemplateType: templateTypeIPFIXOptionsTemplateRecord,
			Data:         templatec,
		})
	case netflow.NFv9OptionsTemplateRecord:
		err = templatesDB.Add(k, templatesValue{
			TemplateType: templateTypeNFv9OptionsTemplateRecord,
			Data:         templatec,
		})
	default:
		return fmt.Errorf("unknown template type: %s", reflect.TypeOf(template).String())
	}
	return err
}

func CreateTemplateSystem(key string) netflow.NetFlowTemplateSystem {
	ts := &TemplateSystem{
		key: key,
	}
	return ts
}

func InitTemplates() error {
	templatesUrl, err := url.Parse(*StateTemplates)
	if err != nil {
		return err
	}
	if !templatesUrl.Query().Has("prefix") {
		q := templatesUrl.Query()
		q.Set("prefix", "goflow2:nf_templates:")
		templatesUrl.RawQuery = q.Encode()
	}
	templatesDB, err = NewState[templatesKey, templatesValue](templatesUrl.String())
	return err
}

func CloseTemplates() error {
	return templatesDB.Close()
}
