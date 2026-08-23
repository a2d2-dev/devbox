package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ReleaseInfo struct {
	CurrentVersion  string    `json:"currentVersion"`
	LatestVersion   string    `json:"latestVersion"`
	UpdateAvailable bool      `json:"updateAvailable"`
	URL             string    `json:"url"`
	PublishedAt     time.Time `json:"publishedAt"`
}

type ReleaseChecker struct {
	Client  *http.Client
	APIBase string
}

func (c ReleaseChecker) Check(ctx context.Context, cfg UpdateConfig, current string) (ReleaseInfo, error) {
	if !cfg.CheckEnabled {
		return ReleaseInfo{CurrentVersion: current}, fmt.Errorf("update checks are disabled")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(cfg.Repository) {
		return ReleaseInfo{}, fmt.Errorf("invalid GitHub repository %q", cfg.Repository)
	}
	base := strings.TrimSuffix(c.APIBase, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/repos/"+cfg.Repository+"/releases/latest", nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "a2d2-devbox-update-check")
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ReleaseInfo{}, fmt.Errorf("GitHub releases request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ReleaseInfo{}, fmt.Errorf("GitHub releases returned HTTP %d", resp.StatusCode)
	}
	var release struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return ReleaseInfo{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	return ReleaseInfo{
		CurrentVersion: current, LatestVersion: release.TagName,
		UpdateAvailable: compareVersions(release.TagName, current) > 0,
		URL:             release.HTMLURL, PublishedAt: release.PublishedAt,
	}, nil
}

func compareVersions(a, b string) int {
	parse := func(v string) []int {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		v = strings.SplitN(v, "-", 2)[0]
		parts := strings.Split(v, ".")
		out := make([]int, 3)
		for i := 0; i < len(parts) && i < len(out); i++ {
			out[i], _ = strconv.Atoi(parts[i])
		}
		return out
	}
	av, bv := parse(a), parse(b)
	for i := range av {
		if av[i] > bv[i] {
			return 1
		}
		if av[i] < bv[i] {
			return -1
		}
	}
	return 0
}
