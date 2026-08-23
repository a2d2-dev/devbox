package shares

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/net/webdav"
)

type WebDAVConfig struct {
	Enabled  bool   `json:"enabled"`
	Port     int    `json:"port"`
	Path     string `json:"path"`
	ReadOnly bool   `json:"readOnly"`
}

type WebDAVStatus struct {
	Running bool   `json:"running"`
	Address string `json:"address,omitempty"`
	Error   string `json:"error,omitempty"`
}

// WebDAVService owns the independent HTTP listener used by the built-in share.
type WebDAVService struct {
	mu     sync.RWMutex
	server *http.Server
	ln     net.Listener
	config WebDAVConfig
	status WebDAVStatus
}

func NewWebDAVService() *WebDAVService { return &WebDAVService{} }

func (s *WebDAVService) Start(dataRoot string, cfg WebDAVConfig, password string) error {
	if !cfg.Enabled {
		return s.Stop(context.Background())
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("WebDAV port must be between 1 and 65535")
	}
	if password == "" {
		return fmt.Errorf("WebDAV requires the console account password")
	}
	root, err := ResolveWithinRoot(dataRoot, cfg.Path)
	if err != nil {
		return err
	}
	cfg.Path = root

	s.mu.RLock()
	oldConfig := s.config
	wasRunning := s.status.Running
	s.mu.RUnlock()
	if wasRunning && oldConfig == cfg {
		return nil
	}

	// When moving ports, reserve the new listener before stopping the healthy
	// old service. A conflict therefore cannot take an existing share offline.
	var ln net.Listener
	if wasRunning && oldConfig.Port != cfg.Port {
		ln, err = net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
		if err != nil {
			return fmt.Errorf("WebDAV port %d is unavailable: %w", cfg.Port, err)
		}
	}
	if err := s.Stop(context.Background()); err != nil {
		if ln != nil {
			ln.Close()
		}
		return err
	}
	if ln == nil {
		ln, err = net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
		if err != nil {
			s.setError(fmt.Sprintf("port %d is unavailable: %v", cfg.Port, err))
			return fmt.Errorf("WebDAV port %d is unavailable: %w", cfg.Port, err)
		}
	}

	var fs webdav.FileSystem = webdav.Dir(root)
	if cfg.ReadOnly {
		fs = readOnlyFS{FileSystem: fs}
	}
	dav := &webdav.Handler{Prefix: "/", FileSystem: fs, LockSystem: webdav.NewMemLS()}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte("devbox")) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="DevBox WebDAV", charset="UTF-8"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if cfg.ReadOnly && isWebDAVWriteMethod(r.Method) {
			http.Error(w, "WebDAV share is read-only", http.StatusForbidden)
			return
		}
		dav.ServeHTTP(w, r)
	})
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	s.mu.Lock()
	s.server = srv
	s.ln = ln
	s.config = cfg
	s.status = WebDAVStatus{Running: true, Address: ln.Addr().String()}
	s.mu.Unlock()

	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			s.setError(serveErr.Error())
		}
	}()
	return nil
}

func (s *WebDAVService) Stop(ctx context.Context) error {
	s.mu.Lock()
	srv := s.server
	s.server = nil
	s.ln = nil
	s.status = WebDAVStatus{}
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("stop WebDAV: %w", err)
	}
	return nil
}

func (s *WebDAVService) Status() WebDAVStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *WebDAVService) Config() WebDAVConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *WebDAVService) setError(message string) {
	s.mu.Lock()
	s.status = WebDAVStatus{Error: message}
	s.mu.Unlock()
}

func isWebDAVWriteMethod(method string) bool {
	switch method {
	case http.MethodPut, http.MethodPost, http.MethodDelete, "MKCOL", "MOVE", "COPY", "PROPPATCH":
		return true
	default:
		return false
	}
}

type readOnlyFS struct{ webdav.FileSystem }

func (readOnlyFS) Mkdir(context.Context, string, os.FileMode) error { return os.ErrPermission }
func (readOnlyFS) RemoveAll(context.Context, string) error          { return os.ErrPermission }
func (readOnlyFS) Rename(context.Context, string, string) error     { return os.ErrPermission }
func (fs readOnlyFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	const writeFlags = os.O_WRONLY | os.O_RDWR | os.O_APPEND | os.O_CREATE | os.O_TRUNC
	if flag&writeFlags != 0 {
		return nil, os.ErrPermission
	}
	return fs.FileSystem.OpenFile(ctx, name, flag, perm)
}
