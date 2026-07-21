package apps

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// 轻量 Docker Engine HTTP 客户端（Issue #2：读操作走 Engine API）。
//
// 刻意不引入 github.com/docker/docker SDK：那会拉入庞大依赖树；本 MVP 只需
// list/inspect containers + version + logs 几个只读端点，net/http 直连足够，
// 且更易测试。
//
// 端点解析（与 docker CLI 的 DOCKER_HOST 一致）：
//   - ""         → 读 DOCKER_HOST 环境变量；仍为空则默认 unix:///var/run/docker.sock
//   - "unix:///path" 或裸 socket 路径 → unix socket
//   - "tcp://host:port" / "http://..." → 普通 HTTP（生产 socket、远程 daemon 均覆盖）
//   - "https://..." 或 DOCKER_TLS_VERIFY=1 → TLS（MVP 用系统/默认证书池）

const defaultDockerSocket = "/var/run/docker.sock"

// dockerEngine Docker Engine 只读客户端。
type dockerEngine struct {
	baseURL string
	client  *http.Client
}

// newDockerEngine 构造客户端（不立即连接，探活在 ping 时）。
func newDockerEngine(endpoint string) *dockerEngine {
	dialAddr, useUnix, baseURL := resolveDockerEndpoint(endpoint)

	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}
	if useUnix {
		sock := dialAddr
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", sock)
		}
	}
	// 非 unix 时用默认 tcp 拨号。

	return &dockerEngine{
		baseURL: baseURL,
		client: &http.Client{
			Transport: tr,
			Timeout:   30 * time.Second,
		},
	}
}

// resolveDockerEndpoint 把 endpoint 解析为 (dialAddr, useUnix, baseURL)。
func resolveDockerEndpoint(endpoint string) (dialAddr string, useUnix bool, baseURL string) {
	if endpoint == "" {
		endpoint = os.Getenv("DOCKER_HOST")
	}
	if endpoint == "" {
		return defaultDockerSocket, true, "http://docker"
	}
	switch {
	case strings.HasPrefix(endpoint, "unix://"):
		return strings.TrimPrefix(endpoint, "unix://"), true, "http://docker"
	case strings.HasPrefix(endpoint, "tcp://"):
		host := strings.TrimPrefix(endpoint, "tcp://")
		return "", false, "http://" + host
	case strings.HasPrefix(endpoint, "http://"), strings.HasPrefix(endpoint, "https://"):
		return "", false, endpoint
	default:
		// 裸路径视为 unix socket。
		return endpoint, true, "http://docker"
	}
}

// ping 探测 daemon 是否可达（尊重调用 ctx，便于超时/取消）。
func (e *dockerEngine) ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+"/_ping", nil)
	if err != nil {
		return fmt.Errorf("docker daemon 不可达 (%s): %w", e.baseURL, err)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		// 给出可读端点信息，便于排查。
		return fmt.Errorf("docker daemon 不可达 (%s): %w", e.baseURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker _ping 返回 %d", resp.StatusCode)
	}
	return nil
}

func (e *dockerEngine) version(ctx context.Context) (string, error) {
	var v struct {
		Version    string `json:"Version"`
		APIVersion string `json:"ApiVersion"`
	}
	if err := e.getJSON(ctx, "/version", nil, &v); err != nil {
		return "", err
	}
	return v.Version, nil
}

// engineContainer /containers/json 的单条结果（仅取需要字段）。
type engineContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
	Ports  []enginePort      `json:"Ports"`
}

type enginePort struct {
	IP          string `json:"IP"`
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort"`
	Type        string `json:"Type"`
}

// listContainers 列出容器（all=true 含停止），可选按 label 过滤。
func (e *dockerEngine) listContainers(ctx context.Context, labelFilters []string) ([]engineContainer, error) {
	q := url.Values{}
	q.Set("all", "1")
	if len(labelFilters) > 0 {
		filters := map[string][]string{"label": labelFilters}
		b, _ := json.Marshal(filters)
		q.Set("filters", string(b))
	}
	var list []engineContainer
	if err := e.getJSON(ctx, "/containers/json", q, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// containerLogs 取容器日志（demultiplex Docker 流式帧）。
func (e *dockerEngine) containerLogs(ctx context.Context, id string, tail int64) (string, error) {
	if id == "" {
		return "", nil
	}
	q := url.Values{}
	q.Set("stdout", "1")
	q.Set("stderr", "1")
	if tail > 0 {
		q.Set("tail", strconv.FormatInt(tail, 10))
	} else {
		q.Set("tail", "200")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+"/containers/"+id+"/logs?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("logs %s: status %d: %s", id, resp.StatusCode, string(b))
	}
	return demuxDockerStream(resp.Body), nil
}

func (e *dockerEngine) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	u := e.baseURL + path
	if q != nil {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("GET %s: status %d: %s", path, resp.StatusCode, string(b))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// demuxDockerStream 解析 Docker 多路复用日志流（[stream:1][0:3][len:4][payload]）。
func demuxDockerStream(r io.Reader) string {
	var out []byte
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			break
		}
		length := binary.BigEndian.Uint32(header[4:8])
		if length == 0 {
			continue
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			break
		}
		out = append(out, payload...)
	}
	return string(out)
}
