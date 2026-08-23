package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnboardingStorePersistsStepState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onboarding.json")
	store := newOnboardingStore(path)
	email := "ops@example.com"
	if _, err := store.update("securityContact", onboardingCompleted, &email); err != nil {
		t.Fatal(err)
	}
	if _, err := store.update("storage", onboardingSkipped, nil); err != nil {
		t.Fatal(err)
	}

	reloaded := newOnboardingStore(path).get()
	if reloaded.Steps["securityContact"] != onboardingCompleted || reloaded.ContactEmail != email {
		t.Fatalf("security contact was not persisted: %+v", reloaded)
	}
	if reloaded.Steps["storage"] != onboardingSkipped {
		t.Fatalf("skipped state was not persisted: %+v", reloaded.Steps)
	}
	if mode := fileMode(t, path); mode != 0600 {
		t.Fatalf("onboarding file mode = %o, want 600", mode)
	}
}

func TestHandleOnboardingSupportsSkipAndRestore(t *testing.T) {
	dir := t.TempDir()
	s := &Server{
		config:     Config{WorkDir: dir},
		onboarding: newOnboardingStore(filepath.Join(dir, "state", "onboarding.json")),
	}

	patch := func(body string) onboardingResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/onboarding", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.handleOnboarding(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH status = %d, body=%s", rec.Code, rec.Body.String())
		}
		var got onboardingResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}

	if got := patch(`{"step":"storage","status":"skipped"}`); got.Steps["storage"] != onboardingSkipped {
		t.Fatalf("skip state = %q", got.Steps["storage"])
	}
	if got := patch(`{"step":"storage","status":"pending"}`); got.Steps["storage"] != onboardingPending {
		t.Fatalf("restored state = %q", got.Steps["storage"])
	}
}

func TestHandleOnboardingRequiresValidContactEmail(t *testing.T) {
	dir := t.TempDir()
	s := &Server{onboarding: newOnboardingStore(filepath.Join(dir, "onboarding.json"))}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/onboarding", strings.NewReader(
		`{"step":"securityContact","status":"completed","contactEmail":"invalid"}`))
	rec := httptest.NewRecorder()
	s.handleOnboarding(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
