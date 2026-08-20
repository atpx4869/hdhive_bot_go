package errors_test

import (
	"errors"
	"testing"

	bizerr "github.com/atpx4869/hdhive_bot_go/internal/errors"
)

func TestUserMessageCoversAllErrors(t *testing.T) {
	allErrors := []error{
		bizerr.ErrResourceNotFound,
		bizerr.ErrResourceAlreadyUnlocked,
		bizerr.ErrUnlockPendingOrInFlight,
		bizerr.ErrUnlockUnknown,
		bizerr.ErrUnlockBusinessRejected,
		bizerr.ErrUnlockNetworkUncertain,
		bizerr.Err115AccountDisabled,
		bizerr.Err115CookieInvalid,
		bizerr.Err115AlreadyReceived,
		bizerr.Err115TransferFailed,
		bizerr.Err115ShareInvalid,
		bizerr.Err115AccessCodeWrong,
		bizerr.ErrTMDBSearchFailed,
		bizerr.ErrTMDBNoResults,
		bizerr.ErrHDHiveSessionFailed,
		bizerr.ErrHDHiveSignatureInvalid,
		bizerr.ErrHDHiveBusinessError,
		bizerr.ErrSessionExpired,
		bizerr.ErrCallbackExpired,
		bizerr.ErrUnauthorized,
		bizerr.ErrAdminRequired,
	}

	for _, err := range allErrors {
		msg := bizerr.UserMessage(err)
		if msg == "" || msg == "❌ 操作暂时失败，请稍后重试。" {
			t.Errorf("UserMessage(%v) = %q, want a specific message", err, msg)
		}
	}
}

func TestUserMessageUnknownError(t *testing.T) {
	msg := bizerr.UserMessage(errors.New("random"))
	if msg != "❌ 操作暂时失败，请稍后重试。" {
		t.Errorf("UserMessage(unknown) = %q", msg)
	}
}

func TestUserMessageChainedError(t *testing.T) {
	err := errors.Join(bizerr.Err115TransferFailed, errors.New("connection refused"))
	msg := bizerr.UserMessage(err)
	if msg != "❌ 115 转存失败，请检查 Cookie 和网络后重试。" {
		t.Errorf("UserMessage(chained) = %q", msg)
	}
}
