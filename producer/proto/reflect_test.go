package protoproducer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetBytes(t *testing.T) {
	d := []byte{0xAA, 0x55, 0xAB, 0x56}

	// Simple case
	r := GetBytes(d, 16, 16, true)
	assert.Equal(t, []byte{0xAB, 0x56}, r)

	r = GetBytes(d, 24, 8, true)
	assert.Equal(t, []byte{0x56}, r)

	r = GetBytes(d, 24, 32, true)
	assert.Equal(t, []byte{0x56, 0x00, 0x00, 0x00}, r)

	// Trying to break
	r = GetBytes(d, 32, 0, true)
	assert.Nil(t, r)

	r = GetBytes(d, 32, 16, true)
	assert.Equal(t, []byte{0x00, 0x00}, r)

	// Offset to shift
	r = GetBytes(d, 4, 16, true)
	assert.Equal(t, []byte{0xA5, 0x5A}, r)

	r = GetBytes(d, 4, 16, false)
	assert.Equal(t, []byte{0xA5, 0x5A}, r)

	r = GetBytes(d, 4, 4, true)
	assert.Equal(t, []byte{0x0A}, r)

	r = GetBytes(d, 4, 4, false)
	assert.Equal(t, []byte{0xA0}, r)

	r = GetBytes(d, 4, 6, true)
	assert.Equal(t, []byte{0x29}, r)

	r = GetBytes(d, 4, 6, false)
	assert.Equal(t, []byte{0xA4}, r)

	r = GetBytes(d, 20, 6, true)
	assert.Equal(t, []byte{0x2D}, r)

	r = GetBytes(d, 20, 6, false)
	assert.Equal(t, []byte{0xB4}, r)

	r = GetBytes(d, 5, 10, true)
	assert.Equal(t, []byte{0x4A, 0x02}, r)

	// Trying to break
	r = GetBytes(d, 30, 10, true)
	assert.Equal(t, []byte{0x80, 0x00}, r)

	r = GetBytes(d, 30, 10, false)
	assert.Equal(t, []byte{0x80, 0x00}, r)

	r = GetBytes(d, 30, 2, true)
	assert.Equal(t, []byte{0x02}, r)

	r = GetBytes(d, 30, 2, false)
	assert.Equal(t, []byte{0x80}, r)

	r = GetBytes(d, 32, 1, true)
	assert.Equal(t, []byte{0}, r)

}

func BenchmarkGetBytes(b *testing.B) {
	d := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	for i := 0; i < b.N; i++ {
		GetBytes(d, 2, 10, false)
	}
}
