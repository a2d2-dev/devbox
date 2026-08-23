package auth

import (
	"testing"
	"time"
)

func TestPruneExpiredSessionsNotifiesCleanupHook(t *testing.T) {
	a := New(Config{Password: "pw", SessionTTL: -1})
	removed := ""
	a.SetSessionRemovedHook(func(token string) { removed = token })
	token := a.NewSession()
	a.PruneExpired()
	if removed != token {
		t.Fatalf("removed token=%q want=%q", removed, token)
	}
	if a.ValidateToken(token) {
		t.Fatal("pruned token still validates")
	}
}

func TestValidateTokenPrunesOtherExpiredSessions(t *testing.T) {
	a := New(Config{Password: "pw", SessionTTL: 60})
	valid := a.NewSession()
	expired := "expired-token"
	a.mu.Lock()
	a.sessions[expired] = time.Now().Add(-time.Minute)
	a.mu.Unlock()

	removed := ""
	a.SetSessionRemovedHook(func(token string) { removed = token })
	if !a.ValidateToken(valid) {
		t.Fatal("valid token was rejected")
	}
	if removed != expired {
		t.Fatalf("removed token=%q want=%q", removed, expired)
	}
	a.mu.RLock()
	_, exists := a.sessions[expired]
	a.mu.RUnlock()
	if exists {
		t.Fatal("expired session remained after validating another token")
	}
}
