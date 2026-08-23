package apps

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// 解析 1Panel 应用级 data.yml（apps/<key>/data.yml）。
func TestParseOnePanelAppData(t *testing.T) {
	raw := `
name: AdGuardHome
tags:
  - 安全
title: 网络范围内的广告和跟踪器阻止 DNS 服务器
description: 网络范围内的广告和跟踪器阻止 DNS 服务器
additionalProperties:
  key: adguardhome
  name: AdGuardHome
  tags:
    - Security
  shortDescZh: 网络范围内的广告和跟踪器阻止 DNS 服务器
  shortDescEn: Network-wide ads & trackers blocking DNS server
  type: website
  website: https://adguard.com/adguard-home.html
  github: https://github.com/AdguardTeam/AdGuardHome
  architectures: [amd64, arm64]
`
	var ad onePanelAppData
	require.NoError(t, yaml.Unmarshal([]byte(raw), &ad))
	require.Equal(t, "adguardhome", ad.AdditionalProperties.Key)
	require.Equal(t, "AdGuardHome", ad.AdditionalProperties.Name)
	require.Equal(t, []string{"Security"}, ad.AdditionalProperties.Tags)
	require.Equal(t, "website", ad.AdditionalProperties.Type)
	require.Equal(t, "https://adguard.com/adguard-home.html", ad.AdditionalProperties.Website)
}

// 解析版本级 data.yml formFields（text/number/password/select 各类型）。
func TestParseOnePanelVersionData(t *testing.T) {
	raw := `
additionalProperties:
  formFields:
    - default: 2FAuth
      envKey: APP_NAME
      required: true
      type: text
    - default: 8000
      envKey: PANEL_APP_PORT_HTTP
      required: true
      rule: paramPort
      type: number
    - default: redis
      envKey: PANEL_REDIS_ROOT_PASSWORD
      required: false
      type: password
    - envKey: PRE_INSTALLED
      required: false
      type: select
      values:
        - label: ffmpeg
          value: "ffmpeg"
`
	var vd onePanelVersionData
	require.NoError(t, yaml.Unmarshal([]byte(raw), &vd))
	fs := vd.AdditionalProperties.FormFields
	require.Len(t, fs, 4)
	require.Equal(t, "PANEL_APP_PORT_HTTP", fs[1].EnvKey)
	require.Equal(t, "number", fs[1].Type)
	require.Equal(t, "password", fs[2].Type)
	require.Equal(t, "select", fs[3].Type)
	require.Len(t, fs[3].Values, 1)
	require.Equal(t, "ffmpeg", fs[3].Values[0].Value)
}

// formFields → devbox valuesSchema + defaults。
// - 类型映射 text/number/password/select→options/boolean。
// - password 不进 schema？不：password 也在 schema（前端展示密码框），但值走 secret。
// - select 缺 values → 错误（不猜）。
// - 未知类型 → 错误（不猜）。
func TestOnePanelFieldsToSchema(t *testing.T) {
	fields := []onePanelFormField{
		{EnvKey: "APP_NAME", Type: "text", Default: "2FAuth", LabelZh: "应用名", LabelEn: "App Name", Required: true},
		{EnvKey: "PANEL_APP_PORT_HTTP", Type: "number", Default: 8000, Required: true},
		{EnvKey: "PANEL_REDIS_ROOT_PASSWORD", Type: "password", Default: "redis"},
		{EnvKey: "MODE", Type: "select", Values: []onePanelSelectOption{{Label: "A", Value: "a"}}, Default: "a"},
		{EnvKey: "ENABLED", Type: "bool", Default: true},
	}
	schemaJSON, defaults, err := onePanelFieldsToSchema(fields)
	require.NoError(t, err)

	var schema storeValuesSchema
	require.NoError(t, json.Unmarshal(schemaJSON, &schema))
	require.Equal(t, "v1", schema.Version)
	require.Len(t, schema.Fields, 5)

	byKey := map[string]storeValueField{}
	for _, f := range schema.Fields {
		byKey[f.Key] = f
	}
	require.Equal(t, "text", byKey["APP_NAME"].Type)
	require.Equal(t, "App Name", byKey["APP_NAME"].Label["en"])
	require.Equal(t, "应用名", byKey["APP_NAME"].Label["zh"])
	require.True(t, byKey["APP_NAME"].Required)
	require.Equal(t, "number", byKey["PANEL_APP_PORT_HTTP"].Type)
	require.Equal(t, "password", byKey["PANEL_REDIS_ROOT_PASSWORD"].Type)
	require.Equal(t, "select", byKey["MODE"].Type)
	require.Len(t, byKey["MODE"].Options, 1)
	require.Equal(t, "a", byKey["MODE"].Options[0].Value)
	require.Equal(t, "boolean", byKey["ENABLED"].Type)

	// defaults: number 保留 JSON number；text/select/boolean 为对应 JSON 值。
	require.JSONEq(t, `8000`, string(defaults["PANEL_APP_PORT_HTTP"]))
	require.JSONEq(t, `"2FAuth"`, string(defaults["APP_NAME"]))
	require.JSONEq(t, `"a"`, string(defaults["MODE"]))
	require.JSONEq(t, `true`, string(defaults["ENABLED"]))
	// 安全（Issue secret 不回传 / 明文敏感默认值阻断）：
	// password 字段仍在 schema（前端展示密码框 + required 由 splitValues 强制），
	// 但其上游 default 一律丢弃——绝不进 DefaultValues / 响应 / revision / audit。
	_, hasPwd := defaults["PANEL_REDIS_ROOT_PASSWORD"]
	require.False(t, hasPwd, "password 默认值不得进入 DefaultValues")
}

// password 上游默认值不得经 StoreAppVersion 序列化泄露到响应。
func TestOnePanelVersion_PasswordDefaultNotSerialized(t *testing.T) {
	fields := []onePanelFormField{
		{EnvKey: "PANEL_REDIS_ROOT_PASSWORD", Type: "password", Default: "leaked-pw-marker", Required: true},
		{EnvKey: "PANEL_APP_PORT_HTTP", Type: "number", Default: 8000, Required: true},
	}
	schemaJSON, defaults, err := onePanelFieldsToSchema(fields)
	require.NoError(t, err)

	ver := StoreAppVersion{
		AppID:           "panelapp",
		Version:         "8.0.0",
		Runtime:         RuntimeCompose,
		Installable:     true,
		ComposeTemplate: "services: {x: {image: app:v8}}",
		ValuesSchema:    schemaJSON,
		DefaultValues:   defaults,
	}
	body, err := json.Marshal(ver)
	require.NoError(t, err)
	// 响应体不得包含上游明文密码默认值；schema 仍含 password 字段（不含其值）。
	require.NotContains(t, string(body), "leaked-pw-marker")
	require.Contains(t, string(body), "PANEL_REDIS_ROOT_PASSWORD")
	require.Contains(t, string(body), `"password"`)
}

// select 缺 values → 错误。
func TestOnePanelFieldsToSchema_SelectRequiresOptions(t *testing.T) {
	_, _, err := onePanelFieldsToSchema([]onePanelFormField{
		{EnvKey: "MODE", Type: "select"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "select")
}

// 未知字段类型 → 错误（不猜）。
func TestOnePanelFieldsToSchema_UnknownTypeRejected(t *testing.T) {
	_, _, err := onePanelFieldsToSchema([]onePanelFormField{
		{EnvKey: "X", Type: "filepath"},
	})
	require.Error(t, err)
}

// password 带非空上游默认值（已丢弃）→ 强制 Required=true；无默认值/空默认且上游 optional → 保留 false。
func TestOnePanelFieldsToSchema_PasswordDefaultForcesRequired(t *testing.T) {
	schemaJSON, defaults, err := onePanelFieldsToSchema([]onePanelFormField{
		{EnvKey: "PW_WITH_DEFAULT", Type: "password", Default: "upstream-leak", Required: false},
		{EnvKey: "PW_NO_DEFAULT", Type: "password", Required: false},
		{EnvKey: "PW_EMPTY_DEFAULT", Type: "password", Default: "", Required: false},
	})
	require.NoError(t, err)
	var schema storeValuesSchema
	require.NoError(t, json.Unmarshal(schemaJSON, &schema))
	byKey := map[string]storeValueField{}
	for _, f := range schema.Fields {
		byKey[f.Key] = f
	}
	require.True(t, byKey["PW_WITH_DEFAULT"].Required, "丢弃非空默认值后必须强制 required")
	require.False(t, byKey["PW_NO_DEFAULT"].Required, "无默认值且上游 optional → 保留 false")
	require.False(t, byKey["PW_EMPTY_DEFAULT"].Required, "空默认值视为无默认 → 保留 false")
	for _, k := range []string{"PW_WITH_DEFAULT", "PW_NO_DEFAULT", "PW_EMPTY_DEFAULT"} {
		_, ok := defaults[k]
		require.False(t, ok, "%s 的 password 默认值不得进入 defaults", k)
	}
}

// sanitizeOnePanelCompose：网络收敛 + container_name 剥离 + ${} 变量转换。
// 断言基于解析后的结构，避免 YAML quoting 造成脆弱。
func TestSanitizeOnePanelCompose(t *testing.T) {
	raw := `
services:
  web:
    container_name: ${CONTAINER_NAME}
    image: myapp:v1.2.3
    restart: always
    networks:
      - 1panel-network
    ports:
      - ${PANEL_APP_PORT_HTTP}:8080/tcp
    environment:
      APP_NAME: ${APP_NAME}
      DB_PASSWORD: ${PANEL_REDIS_ROOT_PASSWORD}
  worker:
    container_name: ${CONTAINER_NAME}-worker
    image: myapp-worker:v1.2.3
    networks:
      - 1panel-network
networks:
  1panel-network:
    external: true
`
	fieldTypes := map[string]string{
		"PANEL_APP_PORT_HTTP":       "number",
		"APP_NAME":                  "text",
		"PANEL_REDIS_ROOT_PASSWORD": "password",
	}
	out, err := sanitizeOnePanelCompose(raw, fieldTypes)
	require.NoError(t, err)

	// container_name 与 ${CONTAINER_NAME} 完全移除。
	require.NotContains(t, out, "container_name")
	require.NotContains(t, out, "CONTAINER_NAME")

	var into map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(out), &into))
	svc := into["services"].(map[string]any)
	web := svc["web"].(map[string]any)
	worker := svc["worker"].(map[string]any)

	_, hasCN := web["container_name"]
	require.False(t, hasCN, "web 不应再含 container_name")
	_, hasCN = worker["container_name"]
	require.False(t, hasCN, "worker 不应再含 container_name")

	// 非秘密字段 ${VAR} → {{ .VAR }}（devbox Go 模板渲染）。
	require.Equal(t, "{{ .PANEL_APP_PORT_HTTP }}:8080/tcp", web["ports"].([]any)[0])
	require.Equal(t, "{{ .APP_NAME }}", web["environment"].(map[string]any)["APP_NAME"])
	// 密码字段保留 ${VAR}（由 docker compose 从 .env 展开，值不进渲染/revision）。
	require.Equal(t, "${PANEL_REDIS_ROOT_PASSWORD}", web["environment"].(map[string]any)["DB_PASSWORD"])

	// 两服务仍引用 1panel-network（收敛为 project-managed，多 service 互通）。
	require.Contains(t, web["networks"].([]any), "1panel-network")
	require.Contains(t, worker["networks"].([]any), "1panel-network")

	// 1panel-network 收敛：external/name 剥离 → Compose 创建 <project>_1panel-network。
	nets := into["networks"].(map[string]any)
	require.Contains(t, nets, "1panel-network")
	opn, _ := nets["1panel-network"].(map[string]any)
	require.Empty(t, opn, "1panel-network 应已去掉 external/name")
}

// 存在 1panel-network 以外的 external network → 视为依赖未知外部服务，报错。
func TestSanitizeOnePanelCompose_OtherExternalNetworkRejected(t *testing.T) {
	raw := `
services:
  web:
    image: x:v1
    networks: [1panel-network, shared-db]
networks:
  1panel-network: {external: true}
  shared-db: {external: true}
`
	_, err := sanitizeOnePanelCompose(raw, map[string]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "外部")
}

// 多 service 通过收敛后的 project-managed 1panel-network 互通（保留引用）。
func TestSanitizeOnePanelCompose_MultiServiceSharedNetwork(t *testing.T) {
	raw := `
services:
  a: {image: x:v1, networks: [1panel-network]}
  b: {image: y:v1, networks: [1panel-network]}
networks:
  1panel-network: {external: true}
`
	out, err := sanitizeOnePanelCompose(raw, nil)
	require.NoError(t, err)
	var into map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(out), &into))
	nets := into["networks"].(map[string]any)
	require.Contains(t, nets, "1panel-network")
	// 收敛后 1panel-network 无 external（Compose 将创建 project 内网络）。
	require.Empty(t, nets["1panel-network"])
}
