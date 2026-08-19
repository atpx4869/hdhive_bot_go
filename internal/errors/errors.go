package errors

import "fmt"

// ErrorCode 业务错误码
type ErrorCode string

const (
	// TMDB 错误
	CodeTMDBSearchFailed ErrorCode = "TMDB_SEARCH_FAILED"
	CodeTMDBRateLimited  ErrorCode = "TMDB_RATE_LIMITED"
	CodeTMDBInvalidToken ErrorCode = "TMDB_INVALID_TOKEN"

	// HDHive 错误
	CodeHDHiveSessionFailed  ErrorCode = "HDHIVE_SESSION_FAILED"
	CodeHDHiveAuthFailed     ErrorCode = "HDHIVE_AUTH_FAILED"
	CodeHDHiveResourceFailed ErrorCode = "HDHIVE_RESOURCE_FAILED"
	CodeHDHiveUnlockFailed   ErrorCode = "HDHIVE_UNLOCK_FAILED"
	CodeHDHiveUnlockRejected ErrorCode = "HDHIVE_UNLOCK_REJECTED"
	CodeHDHiveUnlockUnknown  ErrorCode = "HDHIVE_UNLOCK_UNKNOWN"
	CodeHDHiveInsufficient   ErrorCode = "HDHIVE_INSUFFICIENT_POINTS"
	CodeHDHiveExpired        ErrorCode = "HDHIVE_RESOURCE_EXPIRED"

	// 115 错误
	CodeP115CookieInvalid ErrorCode = "P115_COOKIE_INVALID"
	CodeP115CookieExpired ErrorCode = "P115_COOKIE_EXPIRED"
	CodeP115ShareInvalid  ErrorCode = "P115_SHARE_INVALID"
	CodeP115ShareExpired  ErrorCode = "P115_SHARE_EXPIRED"
	CodeP115ReceiveFailed ErrorCode = "P115_RECEIVE_FAILED"
	CodeP115TargetInvalid ErrorCode = "P115_TARGET_INVALID"
	CodeP115QuotaExceeded ErrorCode = "P115_QUOTA_EXCEEDED"

	// 系统错误
	CodeInternalError    ErrorCode = "INTERNAL_ERROR"
	CodeTimeout          ErrorCode = "TIMEOUT"
	CodeCancelled        ErrorCode = "CANCELLED"
	CodeRateLimited      ErrorCode = "RATE_LIMITED"
	CodeUnauthorized     ErrorCode = "UNAUTHORIZED"
	CodeForbidden        ErrorCode = "FORBIDDEN"
	CodeNotFound         ErrorCode = "NOT_FOUND"
	CodeConflict         ErrorCode = "CONFLICT"
	CodeValidationFailed ErrorCode = "VALIDATION_FAILED"
)

// BusinessError 业务错误
type BusinessError struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *BusinessError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *BusinessError) Unwrap() error {
	return e.Err
}

// New 创建业务错误
func New(code ErrorCode, message string) *BusinessError {
	return &BusinessError{Code: code, Message: message}
}

// Wrap 包装错误
func Wrap(code ErrorCode, message string, err error) *BusinessError {
	return &BusinessError{Code: code, Message: message, Err: err}
}

// UserMessage 返回用户可理解的错误消息
func UserMessage(code ErrorCode) string {
	switch code {
	// TMDB
	case CodeTMDBSearchFailed:
		return "影视搜索失败，请稍后重试。"
	case CodeTMDBRateLimited:
		return "搜索请求过于频繁，请稍后再试。"
	case CodeTMDBInvalidToken:
		return "搜索服务配置错误，请联系管理员。"

	// HDHive
	case CodeHDHiveSessionFailed:
		return "资源服务连接失败，请稍后重试。"
	case CodeHDHiveAuthFailed:
		return "资源服务认证失败，请联系管理员。"
	case CodeHDHiveResourceFailed:
		return "资源查询失败，请稍后重试。"
	case CodeHDHiveUnlockFailed:
		return "解锁失败，请稍后重试。"
	case CodeHDHiveUnlockRejected:
		return "解锁被拒绝，可能是资源失效或权限不足。"
	case CodeHDHiveUnlockUnknown:
		return "解锁结果不确定，请联系管理员。请勿重复付费。"
	case CodeHDHiveInsufficient:
		return "积分不足，无法解锁此资源。"
	case CodeHDHiveExpired:
		return "资源已失效，请选择其他线路。"

	// 115
	case CodeP115CookieInvalid:
		return "115 Cookie 无效，请重新配置。"
	case CodeP115CookieExpired:
		return "115 Cookie 已过期，请重新配置。"
	case CodeP115ShareInvalid:
		return "分享链接无效，请检查后重试。"
	case CodeP115ShareExpired:
		return "分享链接已过期，请重新获取。"
	case CodeP115ReceiveFailed:
		return "转存失败，请稍后重试。"
	case CodeP115TargetInvalid:
		return "目标目录无效，请检查配置。"
	case CodeP115QuotaExceeded:
		return "115 空间不足，请清理后重试。"

	// 系统
	case CodeInternalError:
		return "系统内部错误，请稍后重试。"
	case CodeTimeout:
		return "请求超时，请稍后重试。"
	case CodeCancelled:
		return "操作已取消。"
	case CodeRateLimited:
		return "请求过于频繁，请稍后再试。"
	case CodeUnauthorized:
		return "未授权，请先登录。"
	case CodeForbidden:
		return "无权限执行此操作。"
	case CodeNotFound:
		return "请求的资源不存在。"
	case CodeConflict:
		return "操作冲突，请刷新后重试。"
	case CodeValidationFailed:
		return "输入验证失败，请检查后重试。"

	default:
		return "未知错误，请稍后重试。"
	}
}

// IsRetryable 判断错误是否可重试
func IsRetryable(code ErrorCode) bool {
	switch code {
	case CodeTMDBSearchFailed,
		CodeHDHiveSessionFailed,
		CodeHDHiveResourceFailed,
		CodeHDHiveUnlockFailed,
		CodeP115ReceiveFailed,
		CodeInternalError,
		CodeTimeout:
		return true
	default:
		return false
	}
}

// IsFatal 判断错误是否致命（需要管理员介入）
func IsFatal(code ErrorCode) bool {
	switch code {
	case CodeTMDBInvalidToken,
		CodeHDHiveAuthFailed,
		CodeHDHiveUnlockUnknown,
		CodeP115CookieInvalid,
		CodeP115CookieExpired:
		return true
	default:
		return false
	}
}
