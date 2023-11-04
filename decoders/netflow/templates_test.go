package netflow

import (
	"testing"
)

func benchTemplatesAdd(ts NetFlowTemplateSystem, obs uint32, N int, b *testing.B) {
	for n := 0; n <= N; n++ {
		ts.AddTemplate(10, obs, uint16(n), n)
	}
}

func BenchmarkTemplatesAdd(b *testing.B) {
	ts := CreateTemplateSystem()
	b.Log("Creating", b.N, "templates")
	benchTemplatesAdd(ts, uint32(b.N)%0xffff+1, b.N, b)
}

func BenchmarkTemplatesAddGet(b *testing.B) {
	ts := CreateTemplateSystem()
	templates := 1000
	b.Log("Adding", templates, "templates")
	benchTemplatesAdd(ts, 1, templates, b)
	b.Log("Getting", b.N, "templates")

	for n := 0; n <= b.N; n++ {
		data, err := ts.GetTemplate(10, 1, uint16(n%templates))
		if err != nil {
			b.Fatal(err)
		}
		dataC, ok := data.(int)
		if !ok {
			b.Fatal("template not an integer")
		}
		if dataC != n%templates {
			b.Fatal("different values", dataC, "!=", n%templates)
		}
	}
}
