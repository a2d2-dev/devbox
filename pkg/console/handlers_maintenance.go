package console

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/a2d2-dev/devbox/pkg/maintenance"
	"github.com/a2d2-dev/devbox/pkg/shares"
)

type pendingRestore struct {
	settings  maintenance.Settings
	expiresAt time.Time
}

func (s *Server) registerMaintenanceRoutes() {
	s.mux.HandleFunc("/api/v1/maintenance/settings", s.requireAdminWrites(s.handleMaintenanceSettings))
	s.mux.HandleFunc("/api/v1/maintenance/smb/preview", s.requireAdmin(s.handleSMBPreview))
	s.mux.HandleFunc("/api/v1/maintenance/smb/apply", s.requireAdmin(s.handleSMBApply))
	s.mux.HandleFunc("/api/v1/maintenance/smtp/test", s.requireAdmin(s.handleSMTPTest))
	s.mux.HandleFunc("/api/v1/maintenance/updates/check", s.handleUpdateCheck)
	s.mux.HandleFunc("/api/v1/maintenance/backup", s.requireAdmin(s.handleConfigBackup))
	s.mux.HandleFunc("/api/v1/maintenance/restore/preview", s.requireAdmin(s.handleRestorePreview))
	s.mux.HandleFunc("/api/v1/maintenance/restore/confirm", s.requireAdmin(s.handleRestoreConfirm))
	s.mux.HandleFunc("/api/v1/maintenance/reset", s.requireAdmin(s.handleDevBoxReset))
	s.mux.HandleFunc("/api/v1/maintenance/about", s.handleMaintenanceAbout)
}

func (s *Server) requireMaintenance(w http.ResponseWriter) bool {
	if s.maintenanceStore == nil {
		http.Error(w, "maintenance settings storage is unavailable", http.StatusServiceUnavailable)
		return false
	}
	return true
}

type settingsRequest struct {
	WebDAV       shares.WebDAVConfig      `json:"webdav"`
	SMB          []shares.SMBShare        `json:"smb"`
	SMTP         maintenance.SMTPConfig   `json:"smtp"`
	SMTPPassword string                   `json:"smtpPassword"`
	Updates      maintenance.UpdateConfig `json:"updates"`
	DefaultApps  map[string]string        `json:"defaultApps"`
}

func (s *Server) handleMaintenanceSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireMaintenance(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		state := s.maintenanceStore.Public()
		s.jsonOK(w, map[string]any{
			"settings": state, "webdavStatus": s.webdav.Status(),
			"smbProbe": shares.ProbeSMB(r.Context(), nil),
		})
	case http.MethodPut:
		var req settingsRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, "invalid settings request", http.StatusBadRequest)
			return
		}
		current := s.maintenanceStore.Get()
		next := maintenance.Settings{SchemaVersion: current.SchemaVersion, WebDAV: req.WebDAV, SMB: req.SMB,
			SMTP: req.SMTP, Updates: req.Updates, DefaultApps: req.DefaultApps}
		if req.SMTPPassword == "" {
			next.SMTP.Password = current.SMTP.Password
		} else {
			next.SMTP.Password = req.SMTPPassword
		}
		if _, err := shares.RenderSMB(s.config.WorkDir, next.SMB); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := maintenance.ValidateSettings(next, s.config.WorkDir); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if next.SMTP.Enabled {
			if err := maintenance.ValidateSMTP(next.SMTP); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if err := s.webdav.Start(s.config.WorkDir, next.WebDAV, s.config.AuthPassword); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if err := s.maintenanceStore.Save(next); err != nil {
			_ = s.webdav.Start(s.config.WorkDir, current.WebDAV, s.config.AuthPassword)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonOK(w, map[string]any{"settings": s.maintenanceStore.Public(), "webdavStatus": s.webdav.Status()})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSMBPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var entries []shares.SMBShare
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&entries); err != nil {
		http.Error(w, "invalid SMB shares", http.StatusBadRequest)
		return
	}
	preview, err := shares.RenderSMB(s.config.WorkDir, entries)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.jsonOK(w, map[string]string{"preview": preview})
}

func (s *Server) handleSMBApply(w http.ResponseWriter, r *http.Request) {
	if !s.requireMaintenance(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var entries []shares.SMBShare
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&entries); err != nil {
		http.Error(w, "invalid SMB shares", http.StatusBadRequest)
		return
	}
	target := os.Getenv("DEVBOX_SMB_INCLUDE_PATH")
	result, err := shares.ApplySMB(r.Context(), nil, s.config.WorkDir, target, entries)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	next := s.maintenanceStore.Get()
	next.SMB = entries
	if err := s.maintenanceStore.Save(next); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, result)
}

func (s *Server) handleSMTPTest(w http.ResponseWriter, r *http.Request) {
	if !s.requireMaintenance(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Config   maintenance.SMTPConfig `json:"config"`
		Password string                 `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid SMTP request", http.StatusBadRequest)
		return
	}
	if req.Password == "" {
		req.Config.Password = s.maintenanceStore.Get().SMTP.Password
	} else {
		req.Config.Password = req.Password
	}
	if err := maintenance.SendSMTP(r.Context(), req.Config, "DevBox 测试邮件", "DevBox SMTP 邮件通知配置可用。"); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.jsonOK(w, map[string]string{"message": "测试邮件已发送"})
}

func (s *Server) currentVersion() string {
	if s.collector == nil {
		return "dev"
	}
	version := s.collector.GetDeviceInfo().AgentVersion
	if version == "" {
		return "dev"
	}
	return version
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if !s.requireMaintenance(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	info, err := (maintenance.ReleaseChecker{}).Check(r.Context(), s.maintenanceStore.Get().Updates, s.currentVersion())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.jsonOK(w, info)
}

func (s *Server) handleConfigBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireMaintenance(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	includeSecrets := r.URL.Query().Get("includeSecrets") == "true"
	archive, err := s.maintenanceStore.Export(includeSecrets)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="devbox-config-%s.tar.gz"`, time.Now().UTC().Format("20060102T150405Z")))
	w.Write(archive)
}

func (s *Server) handleRestorePreview(w http.ResponseWriter, r *http.Request) {
	if !s.requireMaintenance(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	archive, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		http.Error(w, "backup upload exceeds 8 MiB", http.StatusRequestEntityTooLarge)
		return
	}
	preview, err := s.maintenanceStore.PreviewRestore(archive)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	token := randomRestoreToken()
	s.restoreMu.Lock()
	s.pendingRestores[token] = pendingRestore{settings: preview.Candidate, expiresAt: time.Now().Add(10 * time.Minute)}
	s.restoreMu.Unlock()
	s.jsonOK(w, map[string]any{"token": token, "changes": preview.Changes, "impact": "将替换 DevBox 文件服务、通知、更新和默认应用配置；执行前自动备份当前配置"})
}

func (s *Server) handleRestoreConfirm(w http.ResponseWriter, r *http.Request) {
	if !s.requireMaintenance(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Token        string `json:"token"`
		Confirmation string `json:"confirmation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Confirmation != "RESTORE" {
		http.Error(w, "confirmation word RESTORE is required", http.StatusBadRequest)
		return
	}
	s.restoreMu.Lock()
	pending, ok := s.pendingRestores[req.Token]
	delete(s.pendingRestores, req.Token)
	s.restoreMu.Unlock()
	if !ok || time.Now().After(pending.expiresAt) {
		http.Error(w, "restore preview is missing or expired", http.StatusConflict)
		return
	}
	backupPath, err := s.maintenanceStore.Restore(pending.settings)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.webdav.Start(s.config.WorkDir, pending.settings.WebDAV, s.config.AuthPassword); err != nil {
		http.Error(w, "configuration restored but WebDAV could not start: "+err.Error(), http.StatusConflict)
		return
	}
	s.jsonOK(w, map[string]string{"message": "配置已还原", "automaticBackup": filepath.Base(backupPath)})
}

func (s *Server) handleDevBoxReset(w http.ResponseWriter, r *http.Request) {
	if !s.requireMaintenance(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Confirm bool   `json:"confirm"`
		Phrase  string `json:"phrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !req.Confirm || req.Phrase != "RESET DEVBOX" {
		http.Error(w, "second confirmation and phrase RESET DEVBOX are required", http.StatusBadRequest)
		return
	}
	_ = s.webdav.Stop(context.Background())
	if err := s.maintenanceStore.Reset(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.restoreMu.Lock()
	s.pendingRestores = make(map[string]pendingRestore)
	s.restoreMu.Unlock()
	s.jsonOK(w, map[string]any{"message": "DevBox 维护配置已恢复出厂值", "osReset": false})
}

func (s *Server) handleMaintenanceAbout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.jsonOK(w, map[string]any{
		"name": "A2D2 DevBox", "version": s.currentVersion(),
		"license": map[string]string{
			"name": "Apache License 2.0", "copyright": "Copyright 2026 A2D2 Devbox Authors",
			"url":  "https://www.apache.org/licenses/LICENSE-2.0",
			"text": "Licensed under the Apache License, Version 2.0. Software is provided on an AS IS basis, without warranties or conditions of any kind.",
		},
		"dependencies": []map[string]string{
			{"name": "React", "license": "MIT"}, {"name": "Vite", "license": "MIT"},
			{"name": "golang.org/x/net/webdav", "license": "BSD-3-Clause"}, {"name": "Viper", "license": "MIT"},
			{"name": "Zap", "license": "MIT"}, {"name": "modernc SQLite", "license": "BSD-3-Clause"},
		},
	})
}

func randomRestoreToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("restore-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
