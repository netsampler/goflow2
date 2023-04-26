package file

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/netsampler/goflow2/decoders/netflow"
	"github.com/netsampler/goflow2/decoders/netflow/templates"
	"github.com/netsampler/goflow2/decoders/netflow/templates/memory"
	"os"
	"sync"
)

type TemplateFileObject struct {
	Key  *templates.TemplateKey
	Data *TemplateFileData
}

type TemplateFileData struct {
	Type string
	Data interface{}
}

func (d *TemplateFileData) UnmarshalJSON(b []byte) error {
	var s struct {
		Type string
		Data interface{} `json:"-"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	switch s.Type {
	case "NFv9OptionsTemplateRecord":
		newS := new(struct {
			Type string
			Data netflow.NFv9OptionsTemplateRecord
		})
		if err := json.Unmarshal(b, newS); err != nil {
			return err
		}
		d.Type = newS.Type
		d.Data = newS.Data
	case "TemplateRecord":
		newS := new(struct {
			Type string
			Data netflow.TemplateRecord
		})
		if err := json.Unmarshal(b, newS); err != nil {
			return err
		}
		d.Type = newS.Type
		d.Data = newS.Data
	case "IPFIXOptionsTemplateRecord":
		newS := new(struct {
			Type string
			Data netflow.IPFIXOptionsTemplateRecord
		})
		if err := json.Unmarshal(b, newS); err != nil {
			return err
		}
		d.Type = newS.Type
		d.Data = newS.Data
	}

	return nil
}

type TemplateFile struct {
	Templates []*TemplateFileObject `json:"templates"`
}

func (f *TemplateFile) Add(key *templates.TemplateKey, data interface{}) {
	var typeName string

	switch data.(type) {
	case netflow.NFv9OptionsTemplateRecord:
		typeName = "NFv9OptionsTemplateRecord"
	case netflow.TemplateRecord:
		typeName = "TemplateRecord"
	case netflow.IPFIXOptionsTemplateRecord:
		typeName = "IPFIXOptionsTemplateRecord"
	default:
		return
	}

	f.Templates = append(f.Templates, &TemplateFileObject{
		Key: key,
		Data: &TemplateFileData{
			Type: typeName,
			Data: data,
		},
	})
}

func NewTemplateFile() *TemplateFile {
	return &TemplateFile{
		Templates: make([]*TemplateFileObject, 0),
	}
}

type FileDriver struct {
	memDriver *memory.MemoryDriver
	path      string
	lock      *sync.Mutex
}

func (d *FileDriver) Prepare() error {
	d.memDriver = memory.Driver
	d.lock = &sync.Mutex{}
	flag.StringVar(&d.path, "netflow.templates.file.path", "./templates.json", "Path of file to store templates")
	return nil
}

func (d *FileDriver) Init(ctx context.Context) error {
	var err error
	if err = d.memDriver.Init(ctx); err != nil {
		return err
	}

	f, err := os.OpenFile(d.path, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	tf := NewTemplateFile()
	if err = dec.Decode(tf); err != nil {
		// log error
	}
	for _, template := range tf.Templates {
		if err := d.memDriver.AddTemplate(ctx, template.Key, template.Data.Data); err != nil {
			// log error
			continue
		}
	}

	return nil
}

func (d *FileDriver) Close(ctx context.Context) error {
	if err := d.memDriver.Close(ctx); err != nil {
		return err
	}
	return nil
}

func (d *FileDriver) ListTemplates(ctx context.Context, ch chan *templates.TemplateKey) error {
	return d.memDriver.ListTemplates(ctx, ch)
}

func (d *FileDriver) AddTemplate(ctx context.Context, key *templates.TemplateKey, template interface{}) error {
	d.lock.Lock()
	defer d.lock.Unlock()
	if err := d.memDriver.AddTemplate(ctx, key, template); err != nil {
		return err
	}

	tf := NewTemplateFile()

	ch := make(chan *templates.TemplateKey, 5)
	go func() {
		if err := d.memDriver.ListTemplates(ctx, ch); err != nil {
			// log error
			close(ch)
		}
	}()
	for key := range ch {
		if key == nil {
			break
		}
		if template, err := d.memDriver.GetTemplate(ctx, key); err != nil {
			// log error
			continue
		} else {
			tf.Add(key, template)
		}

	}

	tmpPath := fmt.Sprintf("%s-tmp", d.path)
	f, err := os.OpenFile(tmpPath, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(f)
	if err := enc.Encode(tf); err != nil {
		f.Close()
		return err
	}

	return os.Rename(tmpPath, d.path)
}

func (d *FileDriver) GetTemplate(ctx context.Context, key *templates.TemplateKey) (interface{}, error) {
	return d.memDriver.GetTemplate(ctx, key)
}

func init() {
	d := &FileDriver{}
	templates.RegisterTemplateDriver("file", d)
}
