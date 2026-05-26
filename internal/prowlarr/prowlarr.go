// Package prowlarr talks to the Prowlarr API (v1). Like Sonarr, the API key is
// read from the local config.xml (zero-config install when running on the
// Prowlarr machine). Prowlarr often listens only on localhost.
package prowlarr

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	APIKey  string
	http    *http.Client
}

func New(baseURL, apiKey string) (*Client, error) {
	if baseURL == "" || apiKey == "" {
		u, k, err := readLocalConfig()
		if err != nil {
			return nil, err
		}
		if baseURL == "" {
			baseURL = u
		}
		if apiKey == "" {
			apiKey = k
		}
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		http: &http.Client{
			Timeout:   12 * time.Second,
			Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext},
		},
	}, nil
}

type xmlConfig struct {
	APIKey  string `xml:"ApiKey"`
	Port    int    `xml:"Port"`
	URLBase string `xml:"UrlBase"`
}

func configPaths() []string {
	var p []string
	if pd := os.Getenv("ProgramData"); pd != "" {
		p = append(p, filepath.Join(pd, "Prowlarr", "config.xml"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		p = append(p,
			filepath.Join(home, "AppData", "Roaming", "Prowlarr", "config.xml"),
			filepath.Join(home, ".config", "Prowlarr", "config.xml"),
		)
	}
	return p
}

func readLocalConfig() (string, string, error) {
	var lastErr error = fmt.Errorf("Prowlarr config.xml not found")
	for _, p := range configPaths() {
		data, e := os.ReadFile(p)
		if e != nil {
			lastErr = e
			continue
		}
		var c xmlConfig
		if e := xml.Unmarshal(data, &c); e != nil {
			lastErr = e
			continue
		}
		if c.APIKey == "" {
			lastErr = fmt.Errorf("ApiKey empty in %s", p)
			continue
		}
		port := c.Port
		if port == 0 {
			port = 9696
		}
		base := fmt.Sprintf("http://localhost:%d", port)
		if ub := strings.Trim(c.URLBase, "/"); ub != "" {
			base += "/" + ub
		}
		return base, c.APIKey, nil
	}
	return "", "", lastErr
}

func (c *Client) req(method, path string, body []byte) ([]byte, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, c.BaseURL+path, r)
	req.Header.Set("X-Api-Key", c.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s -> %s: %s", path, resp.Status, strings.TrimSpace(string(b)))
	}
	return b, nil
}

// Indexer is a simplified view of a Prowlarr indexer for display.
type Indexer struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Enable   bool   `json:"enable"`
	Protocol string `json:"protocol"`
	Privacy  string `json:"privacy"`
	Priority int    `json:"priority"`
}

func (c *Client) Indexers() ([]Indexer, error) {
	b, err := c.req(http.MethodGet, "/api/v1/indexer", nil)
	if err != nil {
		return nil, err
	}
	var ix []Indexer
	if err := json.Unmarshal(b, &ix); err != nil {
		return nil, err
	}
	return ix, nil
}

// SetEnabled enables or disables an indexer. Prowlarr expects the full object
// in PUT, so we re-read it, toggle `enable`, and send it back as-is.
func (c *Client) SetEnabled(id int, enabled bool) error {
	b, err := c.req(http.MethodGet, fmt.Sprintf("/api/v1/indexer/%d", id), nil)
	if err != nil {
		return err
	}
	var full map[string]any
	if err := json.Unmarshal(b, &full); err != nil {
		return err
	}
	full["enable"] = enabled
	body, _ := json.Marshal(full)
	_, err = c.req(http.MethodPut, fmt.Sprintf("/api/v1/indexer/%d", id), body)
	return err
}

// App is an application declared in Prowlarr (Sonarr/Radarr).
type App struct {
	Name           string `json:"name"`
	Implementation string `json:"implementation"`
}

// Applications lists the apps already connected to Prowlarr.
func (c *Client) Applications() ([]App, error) {
	b, err := c.req(http.MethodGet, "/api/v1/applications", nil)
	if err != nil {
		return nil, err
	}
	var apps []App
	if err := json.Unmarshal(b, &apps); err != nil {
		return nil, err
	}
	return apps, nil
}

// AddApplication declares an app (Sonarr or Radarr) in Prowlarr -> Settings ->
// Apps. Once declared, Prowlarr automatically syncs all of its indexers to
// that app (the mechanism that replaces Jackett). Idempotent by name: does
// not recreate it if already present. Returns true if an app was created.
//
// We start from the schema provided by Prowlarr (it contains implementation,
// configContract and the right default sync categories), and only replace
// the name, sync level, and the 3 connection fields.
func (c *Client) AddApplication(name, implementation, prowlarrURL, appBaseURL, appAPIKey string) (bool, error) {
	b, err := c.req(http.MethodGet, "/api/v1/applications", nil)
	if err != nil {
		return false, err
	}
	var existing []map[string]any
	_ = json.Unmarshal(b, &existing)
	for _, a := range existing {
		if s, _ := a["name"].(string); s == name {
			return false, nil
		}
	}

	b, err = c.req(http.MethodGet, "/api/v1/applications/schema", nil)
	if err != nil {
		return false, err
	}
	var schema []map[string]any
	_ = json.Unmarshal(b, &schema)
	var tpl map[string]any
	for _, s := range schema {
		if impl, _ := s["implementation"].(string); impl == implementation {
			tpl = s
			break
		}
	}
	if tpl == nil {
		return false, fmt.Errorf("application schema %q not found in Prowlarr", implementation)
	}

	tpl["name"] = name
	tpl["syncLevel"] = "fullSync"
	if fields, ok := tpl["fields"].([]any); ok {
		for _, f := range fields {
			fm, _ := f.(map[string]any)
			switch fm["name"] {
			case "prowlarrUrl":
				fm["value"] = prowlarrURL
			case "baseUrl":
				fm["value"] = appBaseURL
			case "apiKey":
				fm["value"] = appAPIKey
			}
		}
	}

	body, _ := json.Marshal(tpl)
	// NO forceSave here: we want Prowlarr to actually test the connection to
	// the app. Otherwise a broken app would be saved but never sync. On
	// failure, the error is surfaced to the user.
	if _, err := c.req(http.MethodPost, "/api/v1/applications", body); err != nil {
		return false, err
	}
	return true, nil
}

// SyncApps forces Prowlarr to push its indexers to the connected apps
// (Sonarr/Radarr). Without this, the sync only happens at regular intervals or
// when an indexer changes.
func (c *Client) SyncApps() error {
	_, err := c.req(http.MethodPost, "/api/v1/command", []byte(`{"name":"ApplicationIndexerSync"}`))
	return err
}

// IndexerSchema returns the raw catalog of available indexers.
func (c *Client) IndexerSchema() ([]byte, error) {
	return c.req(http.MethodGet, "/api/v1/indexer/schema", nil)
}

// AddIndexer adds an indexer from the catalog (by name), enabled. We start
// from the schema entry (it contains implementation/configContract/default
// fields) — this works as-is for public indexers; private indexers that
// require credentials will return a validation error.
func (c *Client) AddIndexer(name string) error {
	b, err := c.req(http.MethodGet, "/api/v1/indexer/schema", nil)
	if err != nil {
		return err
	}
	var schema []map[string]any
	if err := json.Unmarshal(b, &schema); err != nil {
		return err
	}
	var tpl map[string]any
	for _, s := range schema {
		if n, _ := s["name"].(string); n == name {
			tpl = s
			break
		}
	}
	if tpl == nil {
		return fmt.Errorf("indexer %q not found in the catalog", name)
	}
	tpl["enable"] = true
	tpl["appProfileId"] = c.defaultAppProfileID() // required by Prowlarr
	body, _ := json.Marshal(tpl)
	// forceSave=true: we save without requiring the connection test to pass
	// (useful for public indexers that are sometimes blocked); configurable afterwards.
	_, err = c.req(http.MethodPost, "/api/v1/indexer?forceSave=true", body)
	return err
}

// defaultAppProfileID returns the ID of the first Prowlarr "App Profile"
// (always present; 1 by default). Prowlarr requires it to create an indexer.
func (c *Client) defaultAppProfileID() int {
	b, err := c.req(http.MethodGet, "/api/v1/appprofile", nil)
	if err == nil {
		var profs []struct {
			ID int `json:"id"`
		}
		if json.Unmarshal(b, &profs) == nil && len(profs) > 0 {
			return profs[0].ID
		}
	}
	return 1
}
