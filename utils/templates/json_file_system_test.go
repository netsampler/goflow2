package templates

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

func TestJSONFileTemplateSystem_PersistsAndLoads(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"

	base := netflow.CreateTemplateSystem()
	writer := NewAtomicFileWriter(path)
	system := NewJSONFileTemplateSystem("router-a", base, writer, 0)

	template := netflow.TemplateRecord{
		TemplateId: 256,
		FieldCount: 1,
		Fields: []netflow.Field{
			{Type: 1, Length: 4},
		},
	}
	if err := system.AddTemplate(9, 1, 256, template); err != nil {
		t.Fatalf("add template: %v", err)
	}
	if flusher, ok := system.(interface {
		Flush()
	}); ok {
		flusher.Flush()
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var stored jsonTemplateFile
	if err := json.Unmarshal(payload, &stored); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}

	routerTemplates, ok := stored.Routers["router-a"]
	if !ok {
		t.Fatalf("missing router entry")
	}
	if _, ok := routerTemplates.Versions["9"]; !ok {
		t.Fatalf("missing version key")
	}

	reloadedBase := netflow.CreateTemplateSystem()
	reloaded := NewJSONFileTemplateSystem("router-a", reloadedBase, writer, 0)
	reloadedTemplate, err := reloaded.GetTemplate(9, 1, 256)
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	if !reflect.DeepEqual(reloadedTemplate, template) {
		t.Fatalf("template mismatch: %#v", reloadedTemplate)
	}
}

func TestJSONFileTemplateSystem_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"

	if err := os.WriteFile(path, []byte("{bad json"), 0o644); err != nil {
		t.Fatalf("write corrupt JSON: %v", err)
	}

	base := netflow.CreateTemplateSystem()
	writer := NewAtomicFileWriter(path)
	system := NewJSONFileTemplateSystem("router-a", base, writer, 0)
	if len(system.GetTemplates()) != 0 {
		t.Fatalf("expected empty templates on corrupt file")
	}
}
