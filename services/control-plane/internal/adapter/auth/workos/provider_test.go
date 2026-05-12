package workos

import (
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// TestProvider_ImplementsAuthProvider is a compile-time conformance check —
// if Provider drifts from the AuthProvider interface, the test won't compile.
func TestProvider_ImplementsAuthProvider(t *testing.T) {
	var _ domain.AuthProvider = (*Provider)(nil)
}
