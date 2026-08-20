// Package errors defines stable business error codes for user-facing messages.
package errors

import "errors"

var (
	ErrResourceNotFound        = errors.New("resource not found")
	ErrResourceAlreadyUnlocked = errors.New("resource already unlocked")
	ErrUnlockPendingOrInFlight = errors.New("unlock pending or in flight")
	ErrUnlockUnknown           = errors.New("unlock result unknown, manual verification required")
	ErrUnlockBusinessRejected  = errors.New("unlock rejected by upstream")
	ErrUnlockNetworkUncertain  = errors.New("unlock network error, result uncertain")

	Err115AccountDisabled = errors.New("115 account is disabled")
	Err115CookieInvalid   = errors.New("115 cookie is invalid or expired")
	Err115AlreadyReceived = errors.New("resource already received on 115")
	Err115TransferFailed  = errors.New("115 transfer failed")
	Err115ShareInvalid    = errors.New("115 share link is invalid or expired")
	Err115AccessCodeWrong = errors.New("115 access code is incorrect")

	ErrTMDBTokenMissing   = errors.New("TMDB token is not configured")
	ErrTMDBSearchFailed   = errors.New("TMDB search failed")
	ErrTMDBNoResults      = errors.New("no TMDB results found")

	ErrHDHiveSessionFailed  = errors.New("HDHive session negotiation failed")
	ErrHDHiveSignatureInvalid = errors.New("HDHive signature verification failed")
	ErrHDHiveBusinessError  = errors.New("HDHive rejected the request")

	ErrSessionExpired   = errors.New("session expired")
	ErrCallbackExpired  = errors.New("callback button expired or belongs to another user")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrAdminRequired    = errors.New("admin permission required")

	ErrConfigMissing = errors.New("required configuration is missing")
)

// UserMessage maps error types to user-facing messages.
func UserMessage(err error) string {
	switch {
	case errors.Is(err, ErrResourceNotFound):
		return "❌ 资源未找到，请重新搜索。"
	case errors.Is(err, ErrResourceAlreadyUnlocked):
		return "✅ 该资源已经解锁，无需重复提交。"
	case errors.Is(err, ErrUnlockPendingOrInFlight):
		return "⏳ 该资源正在处理中，请勿重复点击。"
	case errors.Is(err, ErrUnlockUnknown):
		return "⚠️ 解锁结果不确定，请稍后重新查询资源详情，勿重复付费。"
	case errors.Is(err, ErrUnlockBusinessRejected):
		return "❌ 解锁失败：积分不足、资源失效或账号权限不足。"
	case errors.Is(err, ErrUnlockNetworkUncertain):
		return "⚠️ 网络异常，解锁结果不确定。请稍后重新查询。"
	case errors.Is(err, Err115AccountDisabled):
		return "❌ 115 账号已停用，请先使用 /set115 配置。"
	case errors.Is(err, Err115CookieInvalid):
		return "❌ 115 Cookie 已失效，请重新使用 /set115 配置。"
	case errors.Is(err, Err115AlreadyReceived):
		return "✅ 该分享此前已接收过，未重复转存。"
	case errors.Is(err, Err115TransferFailed):
		return "❌ 115 转存失败，请检查 Cookie 和网络后重试。"
	case errors.Is(err, Err115ShareInvalid):
		return "❌ 分享链接无效或已失效，请重新获取。"
	case errors.Is(err, Err115AccessCodeWrong):
		return "❌ 分享提取码错误。"
	case errors.Is(err, ErrTMDBSearchFailed):
		return "❌ 搜索暂时失败，请稍后重试。"
	case errors.Is(err, ErrTMDBNoResults):
		return "🔍 未找到相关结果。"
	case errors.Is(err, ErrHDHiveSessionFailed):
		return "❌ HDHive 会话建立失败，请检查配置。"
	case errors.Is(err, ErrHDHiveSignatureInvalid):
		return "❌ HDHive 签名验证失败。"
	case errors.Is(err, ErrHDHiveBusinessError):
		return "❌ HDHive 上游拒绝请求。"
	case errors.Is(err, ErrSessionExpired):
		return "⏳ 操作已过期，请重新搜索。"
	case errors.Is(err, ErrCallbackExpired):
		return "⏳ 按钮已过期或不属于你。"
	case errors.Is(err, ErrUnauthorized):
		return "🔒 你尚未获得授权。"
	case errors.Is(err, ErrAdminRequired):
		return "🔒 需要管理员权限。"
	default:
		return "❌ 操作暂时失败，请稍后重试。"
	}
}

// Wrap wraps an error with a business error code for classification.
func Wrap(business, detail error) error {
	if detail != nil && !errors.Is(business, detail) {
		return errors.Join(business, detail)
	}
	return business
}
