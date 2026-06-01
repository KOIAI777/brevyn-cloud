package gatewayerror_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/brevyn/brevyn-cloud/internal/gateway/gatewayerror"
	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
)

func TestClassifyRequestErrors(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  string
		wantClass string
		retryable bool
	}{
		{
			name:      "auth",
			err:       sub2api.RequestError{Method: "POST", Path: "/api/v1/admin/users", StatusCode: 401},
			wantCode:  "gateway_auth_failed",
			wantClass: "auth_error",
		},
		{
			name:      "version mismatch",
			err:       sub2api.RequestError{Method: "POST", Path: "/missing", StatusCode: 404},
			wantCode:  "gateway_endpoint_not_found",
			wantClass: "version_error",
		},
		{
			name:      "rate limited",
			err:       sub2api.RequestError{Method: "POST", Path: "/api/v1/admin/users/1/balance", StatusCode: 429},
			wantCode:  "gateway_rate_limited",
			wantClass: "rate_limited",
			retryable: true,
		},
		{
			name:      "upstream unavailable",
			err:       sub2api.RequestError{Method: "POST", Path: "/api/v1/admin/users/1/balance", StatusCode: 503},
			wantCode:  "gateway_upstream_unavailable",
			wantClass: "transient",
			retryable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := gatewayerror.Classify("add_balance", tt.err)
			if info.Code != tt.wantCode || info.Class != tt.wantClass || info.Retryable != tt.retryable {
				t.Fatalf("unexpected classification: %+v", info)
			}
		})
	}
}

func TestClassifyTimeoutAndConfig(t *testing.T) {
	timeoutErr := gatewayerror.Classify("ensure_user", &net.DNSError{IsTimeout: true})
	if timeoutErr.Class != "transient" || !timeoutErr.Retryable {
		t.Fatalf("timeout should be transient retryable: %+v", timeoutErr)
	}

	configErr := gatewayerror.Classify("settings", errors.New("sub2api base url is not configured"))
	if configErr.Class != "config_error" || configErr.Retryable {
		t.Fatalf("config error should not be retryable: %+v", configErr)
	}

	deadlineErr := gatewayerror.Classify("assign_subscription", context.DeadlineExceeded)
	if deadlineErr.Class != "transient" || !deadlineErr.Retryable {
		t.Fatalf("deadline should be transient retryable: %+v", deadlineErr)
	}
}

func TestClassifyStageWrapper(t *testing.T) {
	info := gatewayerror.Classify("gateway", gatewayerror.WithStage("ensure_api_key", errors.New("connection refused")))
	if info.Stage != "ensure_api_key" {
		t.Fatalf("expected wrapped stage, got %+v", info)
	}
	if info.Class != "transient" || !info.Retryable {
		t.Fatalf("expected transient retryable, got %+v", info)
	}
}
