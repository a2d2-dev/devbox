package files

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Favorite struct {
	Source  string    `json:"source"`
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	IsDir   bool      `json:"isDir"`
	AddedAt time.Time `json:"addedAt"`
}

type Recent struct {
	Source   string    `json:"source"`
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	IsDir    bool      `json:"isDir"`
	OpenedAt time.Time `json:"openedAt"`
}

type Share struct {
	ID        string     `json:"id"`
	Source    string     `json:"source"`
	Path      string     `json:"path"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	TokenHash string     `json:"-"`
}

type CreatedShare struct {
	Share
	Token string `json:"token"`
	URL   string `json:"url"`
}

type persistedShare struct {
	ID        string     `json:"id"`
	Source    string     `json:"source"`
	Path      string     `json:"path"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	TokenHash string     `json:"tokenHash"`
}

type managerState struct {
	Favorites []Favorite       `json:"favorites"`
	Recent    []Recent         `json:"recent"`
	Shares    []persistedShare `json:"shares"`
}

func (b *Browser) loadStateLocked() (managerState, error) {
	var state managerState
	data, err := os.ReadFile(filepath.Join(b.stateDir, "state.json"))
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, fileError("IO_ERROR", "read state: %v", err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fileError("STATE_CORRUPT", "decode state: %v", err)
	}
	return state, nil
}

func (b *Browser) saveStateLocked(state managerState) error {
	if err := os.MkdirAll(b.stateDir, 0o700); err != nil {
		return fileError("IO_ERROR", "create state directory: %v", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fileError("IO_ERROR", "encode state: %v", err)
	}
	tmp, err := os.CreateTemp(b.stateDir, "state-*.json")
	if err != nil {
		return fileError("IO_ERROR", "create state file: %v", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fileError("IO_ERROR", "protect state: %v", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fileError("IO_ERROR", "write state: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return fileError("IO_ERROR", "close state: %v", err)
	}
	if err := os.Rename(tmpName, filepath.Join(b.stateDir, "state.json")); err != nil {
		return fileError("IO_ERROR", "replace state: %v", err)
	}
	return nil
}

func (b *Browser) Favorites() ([]Favorite, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	state, err := b.loadStateLocked()
	return state.Favorites, err
}

func (b *Browser) SetFavorite(sourceID, path string, enabled bool) error {
	source, err := b.require(sourceID, func(c Capabilities) bool { return c.Favorite })
	if err != nil {
		return err
	}
	full, clean, err := b.resolve(source, path, false)
	if err != nil {
		return err
	}
	info, err := os.Stat(full)
	if err != nil {
		return fileError("PATH_NOT_FOUND", "favorite target not found")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	state, err := b.loadStateLocked()
	if err != nil {
		return err
	}
	filtered := state.Favorites[:0]
	found := false
	for _, favorite := range state.Favorites {
		if favorite.Source == source.ID && favorite.Path == clean {
			found = true
			if enabled {
				filtered = append(filtered, favorite)
			}
			continue
		}
		filtered = append(filtered, favorite)
	}
	state.Favorites = filtered
	if enabled && !found {
		state.Favorites = append(state.Favorites, Favorite{Source: source.ID, Path: clean, Name: filepath.Base(clean), IsDir: info.IsDir(), AddedAt: b.now().UTC()})
	}
	return b.saveStateLocked(state)
}

func (b *Browser) recordRecent(sourceID, path string, info os.FileInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	state, err := b.loadStateLocked()
	if err != nil {
		return
	}
	items := []Recent{{Source: sourceID, Path: path, Name: filepath.Base(path), IsDir: info.IsDir(), OpenedAt: b.now().UTC()}}
	for _, recent := range state.Recent {
		if recent.Source == sourceID && recent.Path == path {
			continue
		}
		items = append(items, recent)
		if len(items) == 100 {
			break
		}
	}
	state.Recent = items
	_ = b.saveStateLocked(state)
}

func (b *Browser) Recent() ([]Recent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	state, err := b.loadStateLocked()
	return state.Recent, err
}

func (b *Browser) CreateShare(sourceID, path string, ttl time.Duration) (CreatedShare, error) {
	source, err := b.require(sourceID, func(c Capabilities) bool { return c.Share })
	if err != nil {
		return CreatedShare{}, err
	}
	full, clean, err := b.resolve(source, path, false)
	if err != nil {
		return CreatedShare{}, err
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		return CreatedShare{}, fileError("NOT_FILE", "only regular files can be shared")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return CreatedShare{}, fileError("IO_ERROR", "generate token: %v", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	id, err := randomID(8)
	if err != nil {
		return CreatedShare{}, fileError("IO_ERROR", "generate share id: %v", err)
	}
	created := b.now().UTC()
	var expires *time.Time
	if ttl > 0 {
		value := created.Add(ttl)
		expires = &value
	}
	entry := persistedShare{ID: id, Source: source.ID, Path: clean, Name: info.Name(), CreatedAt: created, ExpiresAt: expires, TokenHash: hex.EncodeToString(hash[:])}
	b.mu.Lock()
	defer b.mu.Unlock()
	state, err := b.loadStateLocked()
	if err != nil {
		return CreatedShare{}, err
	}
	state.Shares = append([]persistedShare{entry}, state.Shares...)
	if err := b.saveStateLocked(state); err != nil {
		return CreatedShare{}, err
	}
	return CreatedShare{Share: publicShare(entry), Token: token, URL: "/api/v1/files/public/" + token}, nil
}

func publicShare(value persistedShare) Share {
	return Share{ID: value.ID, Source: value.Source, Path: value.Path, Name: value.Name, CreatedAt: value.CreatedAt, ExpiresAt: value.ExpiresAt, TokenHash: value.TokenHash}
}

func (b *Browser) Shares() ([]Share, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	state, err := b.loadStateLocked()
	if err != nil {
		return nil, err
	}
	result := make([]Share, 0, len(state.Shares))
	for _, share := range state.Shares {
		result = append(result, publicShare(share))
	}
	return result, nil
}

func (b *Browser) RevokeShare(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	state, err := b.loadStateLocked()
	if err != nil {
		return err
	}
	filtered := state.Shares[:0]
	found := false
	for _, share := range state.Shares {
		if share.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, share)
	}
	if !found {
		return fileError("SHARE_NOT_FOUND", "share not found")
	}
	state.Shares = filtered
	return b.saveStateLocked(state)
}

func (b *Browser) ResolveShare(token string) (string, string, error) {
	if len(token) < 32 || len(token) > 128 {
		return "", "", fileError("SHARE_NOT_FOUND", "share not found")
	}
	hash := sha256.Sum256([]byte(token))
	wanted := hex.EncodeToString(hash[:])
	b.mu.Lock()
	state, err := b.loadStateLocked()
	b.mu.Unlock()
	if err != nil {
		return "", "", err
	}
	for _, share := range state.Shares {
		if len(wanted) != len(share.TokenHash) || subtle.ConstantTimeCompare([]byte(wanted), []byte(share.TokenHash)) != 1 {
			continue
		}
		if share.ExpiresAt != nil && !share.ExpiresAt.After(b.now()) {
			return "", "", fileError("SHARE_EXPIRED", "share has expired")
		}
		full, err := b.ResolveDownload(share.Source, share.Path)
		if err != nil {
			return "", "", err
		}
		return full, share.Name, nil
	}
	return "", "", fileError("SHARE_NOT_FOUND", "share not found")
}

func (b *Browser) appendAudit(action, source, path string) error {
	if err := os.MkdirAll(b.stateDir, 0o700); err != nil {
		return fileError("IO_ERROR", "create audit directory: %v", err)
	}
	event := struct {
		At     time.Time `json:"at"`
		Action string    `json:"action"`
		Source string    `json:"source"`
		Path   string    `json:"path,omitempty"`
	}{At: b.now().UTC(), Action: action, Source: source, Path: path}
	data, _ := json.Marshal(event)
	f, err := os.OpenFile(filepath.Join(b.stateDir, "audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fileError("IO_ERROR", "open audit log: %v", err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
		return fileError("IO_ERROR", "write audit log: %v", err)
	}
	return nil
}

func cleanDisplayPath(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(path), "./")
}
