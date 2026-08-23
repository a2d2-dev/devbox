package auth

import "testing"

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
