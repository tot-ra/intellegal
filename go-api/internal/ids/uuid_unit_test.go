//go:build !integration

package ids

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsUUID_AcceptsCanonicalUUIDsCaseInsensitively(t *testing.T) {
	// arrange

	// act

	// assert
	assert.True(t, IsUUID("550e8400-e29b-41d4-a716-446655440000"))
	assert.True(t, IsUUID("550E8400-E29B-41D4-A716-446655440000"))
}

func TestIsUUID_RejectsMalformedValues(t *testing.T) {
	// arrange
	cases := []string{
		"",
		"not-a-uuid",
		"550e8400e29b41d4a716446655440000",
		"550e8400-e29b-61d4-a716-446655440000",
		"550e8400-e29b-41d4-6716-446655440000",
	}

	// act
	for _, tc := range cases {
		// assert
		assert.False(t, IsUUID(tc), tc)
	}
}

func TestNewUUID_ReturnsValidVersion4UUID(t *testing.T) {
	// arrange

	// act
	value := NewUUID()

	// assert
	assert.True(t, IsUUID(value))
	assert.Equal(t, byte('4'), value[14])
	switch value[19] {
	case '8', '9', 'a', 'b':
	default:
		assert.Failf(t, "expected RFC 4122 variant", "got %q", value)
	}
}
