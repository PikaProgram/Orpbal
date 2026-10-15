package testsupport

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelloWithTestify(t *testing.T) {
	require.NotEmpty(t, Hello(""))
	assert.Equal(t, "Hello, world!", Hello(""))
	assert.Equal(t, "Hello, gopher!", Hello("gopher"))
}
