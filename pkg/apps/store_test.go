package apps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// newTestStoreManager 直接构造 StoreManager（绕过 kubeconfig / InClusterConfig）。
func newTestStoreManager(srvURL string) *StoreManager {
	return &StoreManager{
		apiURL: srvURL,
		logger: zap.NewNop(),
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func fakeCatalog(t *testing.T, handler http.HandlerFunc) *StoreManager {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return newTestStoreManager(srv.URL)
}

func TestListStoreApps_RuntimeAndInstallable(t *testing.T) {
	body := `{"items":[
		{"metadata":{"name":"nginx","labels":{"app.theriseunion.io/provisioner":"workload","app.theriseunion.io/category":"dev-environment","app.theriseunion.io/pinned":"true"},"annotations":{"theriseunion.io/alias-name":"Nginx","theriseunion.io/description":"web server"}},"status":{"latestVersion":"1.2.0","versionCount":3}},
		{"metadata":{"name":"ghost-compose","labels":{"app.theriseunion.io/provisioner":"compose","app.theriseunion.io/category":"ai-tools"},"annotations":{"theriseunion.io/alias-name":"Ghost"}},"status":{"latestVersion":"5.0.0","versionCount":1}}
	]}`
	mgr := fakeCatalog(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	})

	got, err := mgr.ListStoreApps(context.Background())
	if err != nil {
		t.Fatalf("ListStoreApps: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 apps, got %d", len(got))
	}

	nginx := got[0]
	if nginx.Runtime != RuntimeKubernetes {
		t.Errorf("nginx runtime want kubernetes, got %s", nginx.Runtime)
	}
	if nginx.Installable {
		t.Error("nginx should NOT be installable (kubernetes only)")
	}
	if nginx.NotInstallableReason == "" {
		t.Error("nginx should carry not-installable reason")
	}
	if !nginx.Pinned {
		t.Error("nginx should be pinned")
	}
	if nginx.Name != "Nginx" {
		t.Errorf("nginx name want Nginx, got %s", nginx.Name)
	}

	ghost := got[1]
	if ghost.Runtime != RuntimeCompose {
		t.Errorf("ghost runtime want compose, got %s", ghost.Runtime)
	}
	if !ghost.Installable {
		t.Error("ghost should be installable (compose)")
	}
	if ghost.NotInstallableReason != "" {
		t.Errorf("compose app should have empty reason, got %q", ghost.NotInstallableReason)
	}
}

func TestListStoreApps_FallbackName(t *testing.T) {
	body := `{"items":[
		{"metadata":{"name":"raw-id","labels":{},"annotations":{}},"status":{}}
	]}`
	mgr := fakeCatalog(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
	got, err := mgr.ListStoreApps(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "raw-id" {
		t.Fatalf("name should fall back to id, got %+v", got)
	}
}

func TestGetStoreAppVersion_ComposeSelection(t *testing.T) {
	versions := `{"items":[
		{"spec":{"version":"1.0.0","composeTemplate":"services:\n  web:\n    image: nginx\n"},"status":{"reviewPhase":"Approved"}},
		{"spec":{"version":"2.0.0","composeTemplate":"services:\n  web:\n    image: nginx\n"},"status":{"reviewPhase":"Approved"}},
		{"spec":{"version":"0.9.0","composeTemplate":"x"},"status":{"reviewPhase":"Pending"}}
	]}`
	mgr := fakeCatalog(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, versions) })

	// 精确匹配 1.0.0
	v1, err := mgr.GetStoreAppVersion(context.Background(), "ghost", "1.0.0")
	if err != nil {
		t.Fatalf("exact match: %v", err)
	}
	if v1.Version != "1.0.0" {
		t.Errorf("want 1.0.0, got %s", v1.Version)
	}
	if v1.Runtime != RuntimeCompose || !v1.Installable {
		t.Errorf("want compose+installable, got runtime=%s installable=%v", v1.Runtime, v1.Installable)
	}
	if v1.ComposeTemplate == "" {
		t.Error("want compose template populated")
	}

	// version 空 → 取 approved 中最大（2.0.0，排除 pending 0.9.0）
	v2, err := mgr.GetStoreAppVersion(context.Background(), "ghost", "")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if v2.Version != "2.0.0" {
		t.Errorf("want latest 2.0.0, got %s", v2.Version)
	}
}

func TestGetStoreAppVersion_PassthroughSchema(t *testing.T) {
	versions := `{"items":[
		{"spec":{"version":"1.0.0","composeTemplate":"x",
			"valuesSchema":{"version":"v1","fields":[{"key":"tag","type":"text","required":true,"label":{"zh":"标签"}}]},
			"values":{"tag":{"raw":"1.2"}}},
		"status":{"reviewPhase":"Approved"}}
	]}`
	mgr := fakeCatalog(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, versions) })
	v, err := mgr.GetStoreAppVersion(context.Background(), "a", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.ValuesSchema) == 0 {
		t.Error("valuesSchema should pass through")
	}
	if v.DefaultValues["tag"] == nil {
		t.Error("defaultValues should pass through")
	}
}

func TestGetStoreAppVersion_KubernetesRuntime(t *testing.T) {
	versions := `{"items":[
		{"spec":{"version":"1.0.0"},"status":{"reviewPhase":"Approved"}}
	]}`
	mgr := fakeCatalog(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, versions) })
	v, err := mgr.GetStoreAppVersion(context.Background(), "a", "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Runtime != RuntimeKubernetes || v.Installable {
		t.Errorf("want kubernetes+not-installable, got %s/%v", v.Runtime, v.Installable)
	}
	if v.NotInstallableReason == "" {
		t.Error("kubernetes version should carry reason")
	}
}

func TestGetStoreAppVersion_OversizedRejected(t *testing.T) {
	mgr := fakeCatalog(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(make([]byte, maxCatalogBytes+16))
	})
	_, err := mgr.GetStoreAppVersion(context.Background(), "x", "")
	if err == nil {
		t.Fatal("want error for oversized catalog response")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("want size-exceed error, got %v", err)
	}
}

func TestGetStoreAppVersion_NonOK(t *testing.T) {
	mgr := fakeCatalog(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, "upstream error")
	})
	_, err := mgr.GetStoreAppVersion(context.Background(), "x", "")
	if err == nil {
		t.Fatal("want error on non-200")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("want status code in error, got %v", err)
	}
}

func TestGetStoreAppVersion_NoApproved(t *testing.T) {
	mgr := fakeCatalog(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"items":[{"spec":{"version":"1.0.0"},"status":{"reviewPhase":"Pending"}}]}`)
	})
	_, err := mgr.GetStoreAppVersion(context.Background(), "x", "")
	if err == nil {
		t.Fatal("want error when no approved version")
	}
}

func TestGetStoreAppVersion_URLEscaping(t *testing.T) {
	var gotEscaped string
	mgr := fakeCatalog(t, func(w http.ResponseWriter, r *http.Request) {
		gotEscaped = r.URL.EscapedPath()
		fmt.Fprint(w, `{"items":[{"spec":{"version":"1.0.0"},"status":{"reviewPhase":"Approved"}}]}`)
	})
	if _, err := mgr.GetStoreAppVersion(context.Background(), "my app & you", ""); err != nil {
		t.Fatalf("appID with special chars should not break the client: %v", err)
	}
	if strings.Contains(gotEscaped, " ") {
		t.Errorf("appId space not escaped in path: %s", gotEscaped)
	}
	if !strings.Contains(gotEscaped, "%20") {
		t.Errorf("expected percent-encoding in path: %s", gotEscaped)
	}
}

func TestGetStoreAppVersion_EmptyAppID(t *testing.T) {
	mgr := fakeCatalog(t, func(w http.ResponseWriter, r *http.Request) {})
	if _, err := mgr.GetStoreAppVersion(context.Background(), "  ", ""); err == nil {
		t.Fatal("want error for empty appId")
	}
}

// ComposeTemplate 必须被 json:"-" 裁剪，不序列化到对外 JSON。
func TestStoreAppVersion_ComposeTemplateNotSerialized(t *testing.T) {
	v := StoreAppVersion{AppID: "a", Version: "1", Runtime: RuntimeCompose, ComposeTemplate: "SECRET-TEMPLATE"}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "SECRET-TEMPLATE") {
		t.Errorf("compose template leaked to JSON: %s", b)
	}
}
