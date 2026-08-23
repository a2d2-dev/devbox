package auth

import "testing"

func TestExpiredSessionNotifiesCleanup(t *testing.T) {
	a := New(Config{Password: "pw", SessionTTL: -1})
	removed := ""
	a.SetSessionRemovedHook(func(token string) { removed = token })
	token, ok := a.Verify("pw")
	if !ok {
		t.Fatal("password verification failed")
	}
	if a.ValidateToken(token) {
		t.Fatal("already expired token validated")
	}
	if removed != token {
		t.Fatalf("removed token=%q want=%q", removed, token)
	}
}
