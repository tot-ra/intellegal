//go:build !integration

package checksum

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSHA256Hex_ReturnsExpectedDigest(t *testing.T) {
	// arrange
	got := SHA256Hex([]byte("hello world"))
	want := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"

	// act

	// assert
	assert.Equal(t, want, got)
}
