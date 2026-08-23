package maintenance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReleaseChecker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/a2d2-dev/devbox/releases/latest" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.4.0","html_url":"https://example.test/release","published_at":"2026-08-23T00:00:00Z"}`))
	}))
	defer srv.Close()
	got, err := (ReleaseChecker{Client: srv.Client(), APIBase: srv.URL}).Check(context.Background(), UpdateConfig{CheckEnabled: true, Repository: "a2d2-dev/devbox"}, "v1.3.2")
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpdateAvailable || got.LatestVersion != "v1.4.0" {
		t.Fatalf("unexpected release result: %+v", got)
	}
}

func TestReleaseCheckerDisabled(t *testing.T) {
	_, err := (ReleaseChecker{}).Check(context.Background(), UpdateConfig{}, "dev")
	if err == nil {
		t.Fatal("expected disabled update check error")
	}
}
