// Package qbit talks to the qBittorrent WebUI API (v2). Manages the session
// cookie and reconnects if it expires. Compatible with qBittorrent 4.x and 5.x
// (the pause/resume actions were renamed to stop/start in 5.x).
package qbit

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Client struct {
	base string
	user string
	pass string
	http *http.Client

	mu     sync.Mutex
	authed bool
}

func New(base, user, pass string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		base: strings.TrimRight(base, "/"),
		user: user,
		pass: pass,
		http: &http.Client{Timeout: 15 * time.Second, Jar: jar},
	}
}

// Connected checks whether a valid session exists WITHOUT attempting to log
// in (so we don't pile up failures that would get the IP banned by qBittorrent).
// 200 = connected (valid cookie, or localhost auth bypass).
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	req, _ := http.NewRequest(http.MethodGet, c.base+"/api/v2/app/version", nil)
	req.Header.Set("Referer", c.base)
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// Authenticate attempts a login. Returns ErrBanned if the IP is banned,
// another error if the credentials are refused, nil on success.
func (c *Client) Authenticate() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.login()
}

// SetCreds changes the username/password and forces a new session.
func (c *Client) SetCreds(user, pass string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.user = user
	c.pass = pass
	c.authed = false
	if jar, err := cookiejar.New(nil); err == nil {
		c.http.Jar = jar
	}
}

// Detection is what we could deduce about the local qBittorrent install.
type Detection struct {
	Installed    bool
	WebUIEnabled bool
	Port         int
	Username     string
	URL          string
}

// Detect reads the local qBittorrent.ini to deduce the WebUI config (port,
// user, enabled?) and whether qBittorrent is installed. The WebUI password
// there is hashed: it is NOT recoverable (the user must enter it).
func Detect() Detection {
	d := Detection{Username: "admin"}
	if prefs := readQbitPrefs(); prefs != nil {
		d.Installed = true
		d.WebUIEnabled = strings.EqualFold(strings.TrimSpace(prefs["WebUI\\Enabled"]), "true")
		if p, err := strconv.Atoi(strings.TrimSpace(prefs["WebUI\\Port"])); err == nil && p > 0 {
			d.Port = p
		}
		if u := strings.TrimSpace(prefs["WebUI\\Username"]); u != "" {
			d.Username = u
		}
	}
	if !d.Installed && qbitExeExists() {
		d.Installed = true
	}
	if d.Port == 0 {
		d.Port = 8080 // default qBittorrent WebUI port
	}
	d.URL = fmt.Sprintf("http://localhost:%d", d.Port)
	return d
}

// readQbitPrefs returns the keys from the [Preferences] section of qBittorrent.ini.
func readQbitPrefs() map[string]string {
	for _, p := range qbitConfigPaths() {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		prefs := map[string]string{}
		section := ""
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, ";") {
				continue
			}
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				section = strings.ToLower(line[1 : len(line)-1])
				continue
			}
			if section != "preferences" {
				continue
			}
			if i := strings.Index(line, "="); i > 0 {
				prefs[strings.TrimSpace(line[:i])] = strings.TrimSpace(line[i+1:])
			}
		}
		f.Close()
		return prefs
	}
	return nil
}

func qbitConfigPaths() []string {
	var paths []string
	if cfg, err := os.UserConfigDir(); err == nil {
		paths = append(paths,
			filepath.Join(cfg, "qBittorrent", "qBittorrent.ini"),  // Windows: %AppData%\Roaming
			filepath.Join(cfg, "qBittorrent", "qBittorrent.conf"), // Linux: ~/.config
		)
	}
	return paths
}

func qbitExeExists() bool {
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if pf := os.Getenv(env); pf != "" {
			if _, err := os.Stat(filepath.Join(pf, "qBittorrent", "qbittorrent.exe")); err == nil {
				return true
			}
		}
	}
	return false
}

// ErrBanned: qBittorrent has temporarily banned the IP after too many login failures.
var ErrBanned = errors.New("qbit: address temporarily banned by qBittorrent")

func (c *Client) login() error {
	form := url.Values{"username": {c.user}, "password": {c.pass}}
	req, _ := http.NewRequest(http.MethodPost, c.base+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", c.base) // qBittorrent requires a Referer = host
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("qBittorrent connection: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusForbidden {
		return ErrBanned // qBit returns 403 on /auth/login when the IP is banned
	}
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(b)) != "Ok." {
		return fmt.Errorf("qBittorrent credentials refused (%s)", resp.Status)
	}
	c.authed = true
	return nil
}

// do performs a request, (re)connecting the session if needed.
func (c *Client) do(method, path string, form url.Values) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.authed {
		// Best-effort login: if qBittorrent bypasses authentication for
		// localhost / private network (our WebUI config), the call works even
		// without a valid login. So we do not block on a login failure here — a
		// real refusal will surface as a 403 below (which retries a login).
		_ = c.login()
	}
	call := func() (*http.Response, error) {
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req, _ := http.NewRequest(method, c.base+path, body)
		req.Header.Set("Referer", c.base)
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		return c.http.Do(req)
	}
	resp, err := call()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusForbidden { // session expired
		resp.Body.Close()
		c.authed = false
		if err := c.login(); err != nil {
			return nil, err
		}
		if resp, err = call(); err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s -> %s", path, resp.Status)
	}
	c.authed = true // the call succeeded (login OR bypass) -> avoid re-logging in every time
	return b, nil
}

// Torrent is the subset of qBittorrent fields that we care about.
type Torrent struct {
	Hash      string  `json:"hash"`
	Name      string  `json:"name"`
	State     string  `json:"state"`
	Progress  float64 `json:"progress"`
	Size      int64   `json:"size"`
	DlSpeed   int64   `json:"dlspeed"`
	UpSpeed   int64   `json:"upspeed"`
	Ratio     float64 `json:"ratio"`
	NumSeeds  int     `json:"num_seeds"`
	NumLeechs int     `json:"num_leechs"`
	Eta       int64   `json:"eta"`
	AddedOn   int64   `json:"added_on"`
}

func (c *Client) Torrents() ([]Torrent, error) {
	b, err := c.do(http.MethodGet, "/api/v2/torrents/info", nil)
	if err != nil {
		return nil, err
	}
	var ts []Torrent
	if err := json.Unmarshal(b, &ts); err != nil {
		return nil, err
	}
	return ts, nil
}

// setState tries the modern endpoint (qBit 5.x) then falls back to the legacy one (4.x).
func (c *Client) setState(hashes, modern, legacy string) error {
	form := url.Values{"hashes": {hashes}}
	if _, err := c.do(http.MethodPost, "/api/v2/torrents/"+modern, form); err != nil {
		if _, err2 := c.do(http.MethodPost, "/api/v2/torrents/"+legacy, form); err2 != nil {
			return err
		}
	}
	return nil
}

func (c *Client) Pause(hashes string) error  { return c.setState(hashes, "stop", "pause") }
func (c *Client) Resume(hashes string) error { return c.setState(hashes, "start", "resume") }

func (c *Client) Delete(hashes string, deleteFiles bool) error {
	form := url.Values{"hashes": {hashes}, "deleteFiles": {fmt.Sprintf("%t", deleteFiles)}}
	_, err := c.do(http.MethodPost, "/api/v2/torrents/delete", form)
	return err
}

// SetDownloadPaths sets the final download folder and, if provided, the
// temporary folder for in-progress downloads (via the official setPreferences
// API, stable across versions unlike the .ini file).
func (c *Client) SetDownloadPaths(savePath, tempPath string) error {
	prefs := map[string]any{"save_path": savePath}
	if tempPath != "" {
		prefs["temp_path_enabled"] = true
		prefs["temp_path"] = tempPath
	} else {
		prefs["temp_path_enabled"] = false
	}
	j, _ := json.Marshal(prefs)
	_, err := c.do(http.MethodPost, "/api/v2/app/setPreferences", url.Values{"json": {string(j)}})
	return err
}
