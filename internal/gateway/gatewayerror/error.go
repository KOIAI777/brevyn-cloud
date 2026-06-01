package gatewayerror

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

type Info struct {
	Code      string
	Class     string
	Stage     string
	Message   string
	Detail    string
	Retryable bool
}

type StageError struct {
	Stage string
	Err   error
}

func (e StageError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e StageError) Unwrap() error {
	return e.Err
}

func WithStage(stage string, err error) error {
	if err == nil {
		return nil
	}
	return StageError{Stage: strings.TrimSpace(stage), Err: err}
}

func Classify(defaultStage string, err error) Info {
	if err == nil {
		return Info{}
	}
	stage := strings.TrimSpace(defaultStage)
	var staged StageError
	if errors.As(err, &staged) && strings.TrimSpace(staged.Stage) != "" {
		stage = strings.TrimSpace(staged.Stage)
	}
	if stage == "" {
		stage = "gateway"
	}

	detail := strings.TrimSpace(err.Error())
	lower := strings.ToLower(detail)
	info := Info{
		Code:    "gateway_unknown_error",
		Class:   "unknown_gateway_error",
		Stage:   stage,
		Message: "网关同步失败，请查看错误详情",
		Detail:  detail,
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || isNetworkTimeout(err) {
		info.Code = "gateway_timeout"
		info.Class = "transient"
		info.Message = "Sub2API 请求超时或连接中断"
		info.Retryable = true
		return info
	}

	if status, ok := statusCode(err); ok {
		switch {
		case status == 401 || status == 403:
			info.Code = "gateway_auth_failed"
			info.Class = "auth_error"
			info.Message = "Sub2API 管理员鉴权失败"
			return info
		case status == 404:
			info.Code = "gateway_endpoint_not_found"
			info.Class = "version_error"
			info.Message = "Sub2API 接口不存在或版本不匹配"
			return info
		case status == 408 || status == 429:
			info.Code = "gateway_rate_limited"
			info.Class = "rate_limited"
			info.Message = "Sub2API 暂时限流或请求超时"
			info.Retryable = true
			return info
		case status >= 500:
			info.Code = "gateway_upstream_unavailable"
			info.Class = "transient"
			info.Message = fmt.Sprintf("Sub2API 暂时不可用，HTTP %d", status)
			info.Retryable = true
			return info
		case status >= 400:
			info.Code = "gateway_rejected_request"
			info.Class = "data_error"
			info.Message = fmt.Sprintf("Sub2API 拒绝了请求，HTTP %d", status)
			return info
		}
	}

	switch {
	case strings.Contains(lower, "base url is not configured") ||
		strings.Contains(lower, "admin auth is not configured") ||
		strings.Contains(lower, "admin email/password is not configured") ||
		strings.Contains(lower, "not configured"):
		info.Code = "gateway_not_configured"
		info.Class = "config_error"
		info.Message = "Sub2API 连接或管理员凭据未配置"
	case strings.Contains(lower, "login failed") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "forbidden") ||
		strings.Contains(lower, "access token"):
		info.Code = "gateway_auth_failed"
		info.Class = "auth_error"
		info.Message = "Sub2API 登录或授权失败"
	case strings.Contains(lower, "no such host") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "eof") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "temporary failure"):
		info.Code = "gateway_network_error"
		info.Class = "transient"
		info.Message = "Sub2API 网络连接失败"
		info.Retryable = true
	case strings.Contains(lower, "not found"):
		info.Code = "gateway_resource_not_found"
		info.Class = "data_error"
		info.Message = "Sub2API 资源不存在，请检查用户、分组或接口"
	case strings.Contains(lower, "missing group") ||
		strings.Contains(lower, "validity") ||
		strings.Contains(lower, "unsupported") ||
		strings.Contains(lower, "invalid") ||
		strings.Contains(lower, "requires"):
		info.Code = "gateway_invalid_data"
		info.Class = "data_error"
		info.Message = "兑换商品或分组配置不合法"
	}
	return info
}

func isNetworkTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

type httpStatusCarrier interface {
	HTTPStatusCode() int
}

func statusCode(err error) (int, bool) {
	var carrier httpStatusCarrier
	if errors.As(err, &carrier) {
		status := carrier.HTTPStatusCode()
		return status, status > 0
	}
	return 0, false
}
