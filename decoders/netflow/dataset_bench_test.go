package netflow

import (
	"bytes"
	"testing"
)

// benchFixedTemplate returns a template with n fixed 4-byte fields and a
// payload holding records data records for it.
func benchFixedTemplate(n, records int) ([]Field, []byte) {
	fields := make([]Field, n)
	for i := range fields {
		fields[i] = Field{Type: uint16(i + 1), Length: 4}
	}
	payload := make([]byte, 0, n*4*records)
	for r := 0; r < records; r++ {
		for i := 0; i < n; i++ {
			payload = append(payload, byte(r), byte(i), 0, 1)
		}
	}
	return fields, payload
}

// benchVariableTemplate returns a template with n-1 fixed 4-byte fields plus
// one variable-length field, and a payload holding records data records.
func benchVariableTemplate(n, records int) ([]Field, []byte) {
	fields := make([]Field, n)
	for i := range fields[:n-1] {
		fields[i] = Field{Type: uint16(i + 1), Length: 4}
	}
	fields[n-1] = Field{Type: uint16(n), Length: 0xffff}
	var payload []byte
	for r := 0; r < records; r++ {
		for i := 0; i < n-1; i++ {
			payload = append(payload, byte(r), byte(i), 0, 1)
		}
		payload = append(payload, 5, 'a', 'b', 'c', 'd', 'e') // 1-byte length prefix + 5 bytes
	}
	return fields, payload
}

func benchmarkDecodeDataSet(b *testing.B, fields []Field, payload []byte, wantRecords int) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		records, err := DecodeDataSet(10, bytes.NewBuffer(payload), fields)
		if err != nil {
			b.Fatal(err)
		}
		if len(records) != wantRecords {
			b.Fatalf("got %d records, want %d", len(records), wantRecords)
		}
	}
}

func BenchmarkDecodeDataSetFixed30x1(b *testing.B) {
	fields, payload := benchFixedTemplate(30, 1)
	benchmarkDecodeDataSet(b, fields, payload, 1)
}

func BenchmarkDecodeDataSetFixed30x10(b *testing.B) {
	fields, payload := benchFixedTemplate(30, 10)
	benchmarkDecodeDataSet(b, fields, payload, 10)
}

func BenchmarkDecodeDataSetVariable30x10(b *testing.B) {
	fields, payload := benchVariableTemplate(30, 10)
	benchmarkDecodeDataSet(b, fields, payload, 10)
}
