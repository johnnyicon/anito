package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

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
	Name        string               `json:"name"`
	Version     string               `json:"version,omitempty"`
	Type        registry.ServiceType `json:"type"`
	Path        string               `json:"path"`
	Args        []string             `json:"args,omitempty"`
	StablePort  int                  `json:"stable_port"`
	EnvFile     string               `json:"env_file,omitempty"`
	HealthCheck string               `json:"health_check,omitempty"`
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

func parseError(resp *http.Response) error {
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	msg := buf.String()
	if msg == "" {
		msg = resp.Status
	}
	return fmt.Errorf("daemon error %d: %s", resp.StatusCode, msg)
}
