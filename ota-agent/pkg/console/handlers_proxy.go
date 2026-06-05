package console

import (
	"io"
	"net/http"
	"time"
)

// handleProxyAppIcons 代理 /app-icons/* 到 edge-apiserver
func (s *Server) handleProxyAppIcons(w http.ResponseWriter, r *http.Request) {
	if s.storeManager == nil {
		http.Error(w, "store not configured", http.StatusServiceUnavailable)
		return
	}

	// 图标在 console 服务上，用 OAuth URL 的 host（console 地址）
	consoleBase := s.storeManager.GetConsoleURL()
	if consoleBase == "" {
		consoleBase = s.storeManager.GetAPIURL()
	}
	targetURL := consoleBase + r.URL.Path

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(r.Context(), "GET", targetURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	token := s.storeManager.GetToken()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 转发 content-type 和 cache headers
	for _, h := range []string{"Content-Type", "Cache-Control", "ETag"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=86400") // 缓存 1 天
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
