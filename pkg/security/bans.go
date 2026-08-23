package security

import (
	"net"
	"sort"
	"sync"
	"time"
)

type BanRule struct {
	Threshold  int           `json:"threshold"`
	Window     time.Duration `json:"-"`
	BanFor     time.Duration `json:"-"`
	WindowSec  int           `json:"windowSec"`
	BanMinutes int           `json:"banMinutes"`
}

type Ban struct {
	IP       string    `json:"ip"`
	Until    time.Time `json:"until"`
	Failures int       `json:"failures"`
	Source   string    `json:"source"`
}

type BanManager struct {
	mu       sync.Mutex
	rule     BanRule
	failures map[string][]time.Time
	bans     map[string]Ban
	now      func() time.Time
}

func NewBanManager(rule BanRule) *BanManager {
	b := &BanManager{failures: make(map[string][]time.Time), bans: make(map[string]Ban), now: time.Now}
	_ = b.SetRule(rule)
	return b
}

func (b *BanManager) SetClock(now func() time.Time) { b.mu.Lock(); b.now = now; b.mu.Unlock() }

func (b *BanManager) SetRule(rule BanRule) error {
	if rule.Threshold == 0 {
		rule.Threshold = 5
	}
	if rule.Window == 0 {
		rule.Window = time.Duration(rule.WindowSec) * time.Second
	}
	if rule.BanFor == 0 {
		rule.BanFor = time.Duration(rule.BanMinutes) * time.Minute
	}
	if rule.Window == 0 {
		rule.Window = 10 * time.Minute
	}
	if rule.BanFor == 0 {
		rule.BanFor = 30 * time.Minute
	}
	if rule.Threshold < 1 || rule.Threshold > 100 || rule.Window < time.Second || rule.BanFor < time.Second {
		return &ruleError{}
	}
	rule.WindowSec = int(rule.Window.Seconds())
	rule.BanMinutes = int(rule.BanFor.Minutes())
	if rule.BanMinutes == 0 {
		rule.BanMinutes = 1
	}
	b.mu.Lock()
	b.rule = rule
	b.mu.Unlock()
	return nil
}

type ruleError struct{}

func (*ruleError) Error() string { return "invalid ban rule" }

func (b *BanManager) Rule() BanRule { b.mu.Lock(); defer b.mu.Unlock(); return b.rule }

func (b *BanManager) RecordFailure(ip, source string) bool {
	if net.ParseIP(ip) == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	b.expireLocked(now)
	cutoff := now.Add(-b.rule.Window)
	recent := b.failures[ip][:0]
	for _, at := range b.failures[ip] {
		if at.After(cutoff) {
			recent = append(recent, at)
		}
	}
	recent = append(recent, now)
	b.failures[ip] = recent
	if len(recent) >= b.rule.Threshold {
		b.bans[ip] = Ban{IP: ip, Until: now.Add(b.rule.BanFor), Failures: len(recent), Source: source}
		delete(b.failures, ip)
		return true
	}
	return false
}

func (b *BanManager) IsBanned(ip string) (Ban, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.expireLocked(b.now())
	ban, ok := b.bans[ip]
	return ban, ok
}
func (b *BanManager) Unban(ip string) {
	b.mu.Lock()
	delete(b.bans, ip)
	delete(b.failures, ip)
	b.mu.Unlock()
}
func (b *BanManager) List() []Ban {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.expireLocked(b.now())
	out := make([]Ban, 0, len(b.bans))
	for _, ban := range b.bans {
		out = append(out, ban)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Until.Before(out[j].Until) })
	return out
}
func (b *BanManager) expireLocked(now time.Time) {
	for ip, ban := range b.bans {
		if !now.Before(ban.Until) {
			delete(b.bans, ip)
		}
	}
}
