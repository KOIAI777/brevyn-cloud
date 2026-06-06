package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestSafeGatewayProvisionMessageRedactsUpstreamError(t *testing.T) {
	err := officialGatewayCredentialError{
		Code: "gateway_key_sync_failed",
		Err:  errors.New(`sub2api request failed: POST /api/v1/admin/users status=500 body={"token":"secret"}`),
	}

	code := safeGatewayProvisionCode(err)
	message := safeGatewayProvisionMessage(code)

	if code != "gateway_key_sync_failed" {
		t.Fatalf("unexpected code: %s", code)
	}
	for _, leaked := range []string{"sub2api", "/api/v1/admin", "secret"} {
		if strings.Contains(message, leaked) {
			t.Fatalf("message leaked upstream detail %q: %s", leaked, message)
		}
	}
}
