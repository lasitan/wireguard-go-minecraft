package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	Repo        = "lasitan/wireguard-go-minecraft"
	ReleasesURL = "https://github.com/" + Repo + "/releases"
	rawBase     = "https://raw.githubusercontent.com/" + Repo + "/main"

	// InstallScriptURL / InstallPS1URL back the one-line install/update commands.
	InstallScriptURL = rawBase + "/deploy/scripts/install.sh"
	InstallPS1URL    = rawBase + "/deploy/scripts/install.ps1"

	// ProxyEnv prefixes GitHub download URLs (e.g. https://ghfast.top/) for
	// hosts that cannot reach github.com directly.
	ProxyEnv = "WG_MC_GH_PROXY"
)

var latestURL = "https://api.github.com/repos/" + Repo + "/releases/latest"

type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

type Release struct {
	Tag         string    `json:"tag"`
	Version     string    `json:"version"`
	URL         string    `json:"url"`
	Notes       string    `json:"notes"`
	PublishedAt time.Time `json:"publishedAt"`
	Assets      []Asset   `json:"-"`
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
		Digest             string `json:"digest"`
	} `json:"assets"`
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

// Latest fetches the newest published (non-draft, non-prerelease) release.
func Latest(ctx context.Context) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "wireguard-mc/"+orDev(Current()))
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases: %s", resp.Status)
	}
	var gr ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	if gr.Draft || gr.Prerelease || gr.TagName == "" {
		return nil, fmt.Errorf("no published release")
	}
	rel := &Release{
		Tag:         gr.TagName,
		Version:     Normalize(gr.TagName),
		URL:         gr.HTMLURL,
		Notes:       gr.Body,
		PublishedAt: gr.PublishedAt,
	}
	for _, a := range gr.Assets {
		rel.Assets = append(rel.Assets, Asset{
			Name:   a.Name,
			URL:    a.BrowserDownloadURL,
			Size:   a.Size,
			SHA256: strings.TrimPrefix(a.Digest, "sha256:"),
		})
	}
	return rel, nil
}

func (r *Release) Asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// Proxied applies the WG_MC_GH_PROXY prefix to a GitHub download URL.
func Proxied(url string) string {
	p := strings.TrimSpace(os.Getenv(ProxyEnv))
	if p == "" {
		return url
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p + url
}

func orDev(v string) string {
	if v == "" {
		return "dev"
	}
	return v
}
