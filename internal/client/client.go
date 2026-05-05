package client

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/johnnyicon/anito/internal/issues"
	"github.com/johnnyicon/anito/internal/registry"
)

// Client talks to the anito daemon over HTTP.
type Client struct {
	base string // e.g. "http://localhost:7700"
}

func New(port int) *Client {
	return &Client{base: fmt.Sprintf("http://localhost:%d", port)}
}

// DeployRequest mirrors server.DeployRequest.
type DeployRequest struct {
	Name               string               `json:"name"`
	Version            string               `json:"version,omitempty"`
	Type               registry.ServiceType `json:"type"`
	Path               string               `json:"path"`
	Args               []string             `json:"args,omitempty"`
	StablePort         int                  `json:"stable_port"`
	StablePorts        map[string]int       `json:"stable_ports,omitempty"`
	ProxyBindAddress   string               `json:"proxy_bind_address,omitempty"`
	HealthCheckPort    string               `json:"health_check_port,omitempty"`
	EnvFile            string               `json:"env_file,omitempty"`
	HealthCheck        string               `json:"health_check,omitempty"`
	WatchPaths         []string             `json:"watch_paths,omitempty"`
	DrainWindow        string               `json:"drain_window,omitempty"`
	HealthCheckTimeout string               `json:"health_check_timeout,omitempty"`
	RestartPolicy      string               `json:"restart_policy,omitempty"`
	ConfigPath         string               `json:"config_path,omitempty"`
}

func (c *Client) Deploy(req DeployRequest) (*registry.Service, error) {
	var svc registry.Service
	if err := c.postJSON("/deploy", req, &svc); err != nil {
		return nil, err
	}
	return &svc, nil
}

func (c *Client) Services() ([]*registry.Service, error) {
	var svcs []*registry.Service
	if err := c.getJSON("/services", &svcs); err != nil {
		return nil, err
	}
	return svcs, nil
}

func (c *Client) Status(name string) (*registry.Service, error) {
	var svc registry.Service
	if err := c.getJSON("/status/"+name, &svc); err != nil {
		return nil, err
	}
	return &svc, nil
}

func (c *Client) Stop(name string) error {
	return c.postJSON("/stop/"+name, nil, nil)
}

func (c *Client) Restart(name string) error {
	return c.postJSON("/restart/"+name, nil, nil)
}

func (c *Client) Remove(name string) error {
	return c.postJSON("/remove/"+name, nil, nil)
}

func (c *Client) DaemonVersion() (string, error) {
	var result map[string]string
	if err := c.getJSON("/health", &result); err != nil {
		return "", err
	}
	return result["version"], nil
}

func (c *Client) Logs(name string, lines int) ([]string, error) {
	var out []string
	path := fmt.Sprintf("/logs/%s?lines=%d", name, lines)
	if err := c.getJSON(path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// LogsFollow streams log lines from the daemon via SSE, writing each line to w.
// It blocks until the connection is closed or an error occurs.
func (c *Client) LogsFollow(name string, backlog int, w io.Writer) error {
	url := fmt.Sprintf("%s/logs/%s?lines=%d&follow=true", c.base, name, backlog)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("daemon unreachable — is anito running? (try: anito daemon): %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return parseError(resp)
	}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			fmt.Fprintln(w, after)
		}
	}
	return scanner.Err()
}

func (c *Client) getJSON(path string, out any) error {
	resp, err := http.Get(c.base + path)
	if err != nil {
		return fmt.Errorf("daemon unreachable — is anito running? (try: anito daemon): %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return parseError(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) postJSON(path string, body any, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	resp, err := http.Post(c.base+path, "application/json", &buf)
	if err != nil {
		return fmt.Errorf("daemon unreachable — is anito running? (try: anito daemon): %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return parseError(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) Issues(n int, source string) ([]issues.Issue, error) {
	path := fmt.Sprintf("/issues?lines=%d", n)
	if source != "" {
		path += "&source=" + source
	}
	var out struct {
		Issues []issues.Issue `json:"issues"`
	}
	if err := c.getJSON(path, &out); err != nil {
		return nil, err
	}
	return out.Issues, nil
}

func (c *Client) Report(iss issues.Issue) error {
	return c.postJSON("/issues", iss, nil)
}

func (c *Client) Teardown(repoPath string) ([]string, error) {
	var out struct {
		Removed []string `json:"removed"`
	}
	if err := c.postJSON("/teardown", map[string]string{"repo_path": repoPath}, &out); err != nil {
		return nil, err
	}
	return out.Removed, nil
}

func parseError(resp *http.Response) error {
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	msg := buf.String()
	if msg == "" {
		msg = resp.Status
	}
	return fmt.Errorf("daemon error %d: %s", resp.StatusCode, msg)
}
