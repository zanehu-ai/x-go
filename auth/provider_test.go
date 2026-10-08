package auth_test

import (
	"context"
	"errors"
	"testing"

	coreauth "github.com/zanehu-ai/x-go/auth"
)

// MockProviderAdapter is a test double for coreauth.ProviderAdapter.
// It is exported so that identity package tests can import it from
// x-go/auth_test (or copy the pattern locally).
//
// Usage:
//
//	mock := &MockProviderAdapter{
//	    NameValue: "wechat",
//	    OpenID:    "oABC",
//	    AccessToken: "sess_key_123",
//	}
type MockProviderAdapter struct {
	NameValue   string
	OpenID      string
	AccessToken string
	Nickname    string
	AvatarURL   string

	ExchangeErr  error
	FetchUserErr error

	ExchangeCalls  []ExchangeCall
	FetchUserCalls []string // access tokens passed
}

// ExchangeCall records one ExchangeCode invocation for assertion.
type ExchangeCall struct {
	TenantCode string
	Code       string
	State      string
}

func (m *MockProviderAdapter) Name() string { return m.NameValue }

func (m *MockProviderAdapter) ExchangeCode(_ context.Context, tenantCode, code, state string) (string, string, error) {
	m.ExchangeCalls = append(m.ExchangeCalls, ExchangeCall{TenantCode: tenantCode, Code: code, State: state})
	return m.OpenID, m.AccessToken, m.ExchangeErr
}

func (m *MockProviderAdapter) FetchUserInfo(_ context.Context, accessToken string) (string, string, error) {
	m.FetchUserCalls = append(m.FetchUserCalls, accessToken)
	return m.Nickname, m.AvatarURL, m.FetchUserErr
}

// Compile-time interface check.
var _ coreauth.ProviderAdapter = (*MockProviderAdapter)(nil)

// TestProviderAdapterInterface verifies the interface can be satisfied by a mock.
func TestProviderAdapterInterface(t *testing.T) {
	mock := &MockProviderAdapter{
		NameValue:   "wechat",
		OpenID:      "oTEST",
		AccessToken: "sess_key_abc",
		Nickname:    "",
		AvatarURL:   "",
	}

	if mock.Name() != "wechat" {
		t.Fatalf("Name() = %q, want wechat", mock.Name())
	}

	openid, tok, err := mock.ExchangeCode(context.Background(), "dnf-cn", "jscode_xyz", "")
	if err != nil {
		t.Fatalf("ExchangeCode error: %v", err)
	}
	if openid != "oTEST" || tok != "sess_key_abc" {
		t.Fatalf("ExchangeCode = (%q, %q), want (oTEST, sess_key_abc)", openid, tok)
	}
	if len(mock.ExchangeCalls) != 1 || mock.ExchangeCalls[0].TenantCode != "dnf-cn" {
		t.Fatalf("ExchangeCalls = %+v, want one call with tenant dnf-cn", mock.ExchangeCalls)
	}

	nick, avatar, err := mock.FetchUserInfo(context.Background(), tok)
	if err != nil {
		t.Fatalf("FetchUserInfo error: %v", err)
	}
	if nick != "" || avatar != "" {
		t.Fatalf("FetchUserInfo = (%q, %q), want empty (wechat mini-program no-op)", nick, avatar)
	}
}

// TestProviderAdapterErrorPropagation verifies error passthrough.
func TestProviderAdapterErrorPropagation(t *testing.T) {
	wantErr := errors.New("upstream error")
	mock := &MockProviderAdapter{
		NameValue:   "wechat",
		ExchangeErr: wantErr,
	}

	_, _, err := mock.ExchangeCode(context.Background(), "dnf-cn", "bad_code", "")
	if !errors.Is(err, wantErr) {
		t.Fatalf("ExchangeCode error = %v, want %v", err, wantErr)
	}
}
