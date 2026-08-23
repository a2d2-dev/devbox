package apps

import (
	"encoding/json"
	"strings"
	"testing"
)

func renderVer(tmpl string) StoreAppVersion {
	return StoreAppVersion{
		Runtime:         RuntimeCompose,
		Installable:     true,
		ComposeTemplate: tmpl,
		ValuesSchema: json.RawMessage(`{"version":"v1","fields":[
			{"key":"tag","type":"text","label":{"zh":"标签"}},
			{"key":"replicas","type":"number","label":{"zh":"副本"}},
			{"key":"mode","type":"select","options":[{"label":"A","value":"a"},{"label":"B","value":"b"}],"label":{"zh":"模式"}},
			{"key":"debug","type":"boolean","label":{"zh":"调试"}},
			{"key":"pw","type":"password","required":true,"label":{"zh":"密码"}}
		]}`),
	}
}

func TestRenderStoreCompose_Success(t *testing.T) {
	ver := renderVer("services:\n  web:\n    image: nginx:{{ .tag }}\n    environment:\n      REPLICAS: \"{{ .replicas }}\"\n      PW: ${pw}\n")
	input := map[string]any{
		"tag":      "1.25",
		"replicas": float64(2),
		"mode":     "a",
		"debug":    true,
		"pw":       "s3cr3t",
	}
	compose, params, secrets, err := RenderStoreCompose(ver, input)
	if err != nil {
		t.Fatalf("RenderStoreCompose: %v", err)
	}
	// 非敏感参数插值成功。
	if !strings.Contains(compose, "nginx:1.25") {
		t.Errorf("tag not rendered: %s", compose)
	}
	if !strings.Contains(compose, `REPLICAS: "2"`) {
		t.Errorf("number not rendered as int string: %s", compose)
	}
	if params["tag"] != "1.25" || params["replicas"] != "2" || params["mode"] != "a" || params["debug"] != "true" {
		t.Errorf("unexpected params: %+v", params)
	}
	// secret 分离：进 secrets，不进 params，不进 compose 正文（${pw} 保留为 env 引用）。
	if _, leaked := params["pw"]; leaked {
		t.Error("password leaked into params")
	}
	if secrets["pw"] != "s3cr3t" {
		t.Errorf("secret want s3cr3t, got %q", secrets["pw"])
	}
	if strings.Contains(compose, "s3cr3t") {
		t.Errorf("secret value leaked into compose: %s", compose)
	}
	if !strings.Contains(compose, "${pw}") {
		t.Errorf("secret env reference should be preserved: %s", compose)
	}
}

func TestRenderStoreCompose_MissingRequired(t *testing.T) {
	ver := renderVer("image: {{ .tag }}\n")
	_, _, _, err := RenderStoreCompose(ver, map[string]any{"tag": "1.25"}) // 缺 pw
	if err == nil {
		t.Fatal("want error for missing required field")
	}
}

func TestRenderStoreCompose_MissingKey(t *testing.T) {
	ver := StoreAppVersion{
		Runtime:         RuntimeCompose,
		Installable:     true,
		ComposeTemplate: "image: {{ .nope }}\n",
		ValuesSchema:    json.RawMessage(`{"fields":[]}`),
	}
	_, _, _, err := RenderStoreCompose(ver, map[string]any{})
	if err == nil {
		t.Fatal("want missingkey=error for undeclared template var")
	}
}

func TestRenderStoreCompose_UnknownKeyRejected(t *testing.T) {
	ver := renderVer("image: {{ .tag }}\n")
	_, _, _, err := RenderStoreCompose(ver, map[string]any{"tag": "1.25", "pw": "x", "bogus": "y"})
	if err == nil || !strings.Contains(err.Error(), "未声明") {
		t.Fatalf("want undeclared-key error, got %v", err)
	}
}

func TestRenderStoreCompose_NumberValidation(t *testing.T) {
	ver := renderVer("r: {{ .replicas }}\n")
	_, _, _, err := RenderStoreCompose(ver, map[string]any{"replicas": "abc", "pw": "x"})
	if err == nil {
		t.Fatal("want error for non-numeric number field")
	}
}

func TestRenderStoreCompose_SelectValidation(t *testing.T) {
	ver := renderVer("m: {{ .mode }}\n")
	_, _, _, err := RenderStoreCompose(ver, map[string]any{"mode": "zzz", "pw": "x"})
	if err == nil {
		t.Fatal("want error for invalid select option")
	}
}

func TestRenderStoreCompose_BooleanCoercion(t *testing.T) {
	ver := renderVer("d: {{ .debug }}\n")
	_, params, _, err := RenderStoreCompose(ver, map[string]any{"debug": "true", "pw": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if params["debug"] != "true" {
		t.Errorf("boolean coercion failed: %q", params["debug"])
	}
}

func TestRenderStoreCompose_NotInstallable(t *testing.T) {
	ver := StoreAppVersion{Runtime: RuntimeKubernetes, Installable: false, NotInstallableReason: "仅 Kubernetes 环境支持"}
	_, _, _, err := RenderStoreCompose(ver, nil)
	if err == nil {
		t.Fatal("want error for non-installable version")
	}
}

func TestRenderStoreCompose_OutputLimit(t *testing.T) {
	big := "x"
	for len(big) < maxRenderOutput+16 {
		big += big
	}
	ver := StoreAppVersion{Runtime: RuntimeCompose, Installable: true, ComposeTemplate: big, ValuesSchema: json.RawMessage(`{"fields":[]}`)}
	_, _, _, err := RenderStoreCompose(ver, map[string]any{})
	if err == nil {
		t.Fatal("want error for output exceeding limit")
	}
}

func TestRenderStoreCompose_EmptyTemplate(t *testing.T) {
	ver := StoreAppVersion{Runtime: RuntimeCompose, Installable: true, ComposeTemplate: "  \n", ValuesSchema: json.RawMessage(`{"fields":[]}`)}
	_, _, _, err := RenderStoreCompose(ver, map[string]any{})
	if err == nil {
		t.Fatal("want error for empty compose template")
	}
}

func TestParseValuesSchema_InvalidKey(t *testing.T) {
	bad := json.RawMessage(`{"fields":[{"key":"bad key","type":"text"}]}`)
	if _, err := parseValuesSchema(bad); err == nil {
		t.Fatal("want error for illegal field key (env/template safety)")
	}
}

func TestParseValuesSchema_SelectRequiresOptions(t *testing.T) {
	bad := json.RawMessage(`{"fields":[{"key":"mode","type":"select"}]}`)
	if _, err := parseValuesSchema(bad); err == nil {
		t.Fatal("want error for select without options")
	}
	good := json.RawMessage(`{"fields":[{"key":"mode","type":"select","options":[{"label":"A","value":"a"}]}]}`)
	if _, err := parseValuesSchema(good); err != nil {
		t.Fatalf("valid select should parse: %v", err)
	}
}
