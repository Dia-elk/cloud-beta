// Package deploy starts real per-app containers through the Docker API.
//
// It talks to the Docker daemon over its unix socket (DOCKER_HOST,
// default unix:///var/run/docker.sock) or over plain HTTP for tests
// (DOCKER_HOST=http://host:port). It never silently falls back to
// marking an app deployed: if the socket/host is missing or
// unreachable, Start returns an error wrapping ErrDockerUnavailable
// and the caller must surface that (HTTP 503) instead of a fake URL.
// No tokens, DSNs or JWTs are ever logged here.
package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrDockerUnavailable marks a missing or unreachable Docker daemon.
// Callers check errors.Is(err, ErrDockerUnavailable) to report 503
// (docker runtime not available) instead of a generic 502 failure.
var ErrDockerUnavailable = errors.New("docker-unavailable")

// Deployer is the interface the gateway depends on, so unit tests can
// substitute a fake without a Docker daemon.
type Deployer interface {
	Start(appID, companyID, image string) (containerID string, hostPort int, url string, err error)
	Stop(containerID string) error
	Remove(containerID string) error
	InspectStatus(containerID string) (string, error)
	HealthCheck(url string) bool
}

// Client is a minimal Docker API client (create/start/stop/remove/
// inspect/list). It uses only the endpoints the gateway needs.
type Client struct {
	base string
	http *http.Client
}

// NewFromEnv builds a client from DOCKER_HOST. For a unix socket it
// verifies the socket file exists here, so a missing daemon fails
// fast with ErrDockerUnavailable instead of at first container start.
func NewFromEnv() (*Client, error) {
	host := os.Getenv("DOCKER_HOST")
	if strings.TrimSpace(host) == "" {
		host = "unix:///var/run/docker.sock"
	}
	return New(host)
}

// New builds a client for one DOCKER_HOST value:
//   - unix:///var/run/docker.sock (or a bare /path/to/docker.sock)
//   - tcp://host:2375, http://host:2375, https://host:2375
func New(host string) (*Client, error) {
	if strings.HasPrefix(host, "unix://") {
		sock := strings.TrimPrefix(host, "unix://")
		if _, err := os.Stat(sock); err != nil {
			return nil, fmt.Errorf("%w: docker socket %s unreachable: %v", ErrDockerUnavailable, sock, err)
		}
		tr := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
		}
		return &Client{base: "http://localhost", http: &http.Client{Transport: tr, Timeout: 30 * time.Second}}, nil
	}
	if strings.HasPrefix(host, "/") {
		return New("unix://" + host)
	}
	base := host
	if strings.HasPrefix(base, "tcp://") {
		base = "http://" + strings.TrimPrefix(base, "tcp://")
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return &Client{base: strings.TrimSuffix(base, "/"), http: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (c *Client) do(method, path string, body any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// A transport error means the daemon cannot be reached at all.
		return 0, nil, fmt.Errorf("%w: docker host unreachable: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}

// portRange reads DEPLOY_PORT_MIN/MAX, defaulting to 3100-3199.
// Invalid values fall back to the defaults rather than failing open
// to an unbounded range.
// PortRange exposes the deploy port range, e.g. so the gateway can
// allow deployed apps' origins in CORS.
func PortRange() (int, int) { return portRange() }

func portRange() (int, int) {
	min, max := 3100, 3199
	if v := os.Getenv("DEPLOY_PORT_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 65535 {
			min = n
		}
	}
	if v := os.Getenv("DEPLOY_PORT_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 65535 {
			max = n
		}
	}
	if min > max {
		return 3100, 3199
	}
	return min, max
}

type portBinding struct {
	HostPort string `json:"HostPort"`
}

type containerSummary struct {
	ID string `json:"Id"`
	// Docker reports bindings in two places; accept both.
	HostConfig struct {
		PortBindings map[string][]portBinding `json:"PortBindings"`
	} `json:"HostConfig"`
	NetworkSettings struct {
		Ports map[string][]portBinding `json:"NetworkSettings"`
	} `json:"NetworkSettings"`
}

// usedPorts lists host ports already bound by existing containers.
func (c *Client) usedPorts() (map[int]bool, error) {
	code, b, err := c.do("GET", "/containers/json?all=1", nil)
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("docker list containers: HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	var list []containerSummary
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("docker list containers: bad response: %w", err)
	}
	used := map[int]bool{}
	collect := func(m map[string][]portBinding) {
		for _, binds := range m {
			for _, bind := range binds {
				if p, err := strconv.Atoi(bind.HostPort); err == nil {
					used[p] = true
				}
			}
		}
	}
	for _, ct := range list {
		collect(ct.HostConfig.PortBindings)
		collect(ct.NetworkSettings.Ports)
	}
	return used, nil
}

// firstFree returns the first port in [min,max] not in used, or an error.
func firstFree(used map[int]bool, min, max int) (int, error) {
	for p := min; p <= max; p++ {
		if !used[p] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free deploy port in range %d-%d", min, max)
}

func sanitise(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := b.String()
	if len(out) > 50 {
		out = out[:50]
	}
	if out == "" {
		out = "app"
	}
	return out
}

func isBindConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "port is already allocated") ||
		strings.Contains(msg, "address already in use")
}

// Start creates and starts a container for the app, binding container
// port 3000/tcp to the first free host port in the deploy range. On a
// bind conflict it removes the failed container and retries the next
// port. Any other failure is returned as-is: no fake success.
func (c *Client) Start(appID, companyID, image string) (string, int, string, error) {
	if image == "" {
		image = "cloudbeta-sample-app"
	}
	gatewayURL := os.Getenv("NEXT_PUBLIC_GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://localhost:8080"
	}
	used, err := c.usedPorts()
	if err != nil {
		return "", 0, "", err
	}
	min, max := portRange()
	name := "cloudbeta-" + sanitise(appID)
	for port := min; port <= max; port++ {
		if used[port] {
			continue
		}
		created, err := c.create(name, image, companyID, appID, port, gatewayURL)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "already in use") && strings.Contains(err.Error(), "409") {
				// Stale container holds this app's name: remove and retry once.
				_ = c.Remove(name)
				created, err = c.create(name, image, companyID, appID, port, gatewayURL)
			}
			if err != nil {
				return "", 0, "", err
			}
		}
		if err := c.start(created); err != nil {
			_ = c.Remove(created)
			if isBindConflict(err) {
				used[port] = true
				continue
			}
			return "", 0, "", err
		}
		return created, port, "http://localhost:" + strconv.Itoa(port), nil
	}
	return "", 0, "", fmt.Errorf("no free deploy port in range %d-%d", min, max)
}

func (c *Client) create(name, image, companyID, appID string, hostPort int, gatewayURL string) (string, error) {
	body := map[string]any{
		"Image": image,
		"Env":   []string{"NEXT_PUBLIC_GATEWAY_URL=" + gatewayURL},
		"Labels": map[string]string{
			"com.cloudbeta.app":     appID,
			"com.cloudbeta.company": companyID,
		},
		"ExposedPorts": map[string]any{"3000/tcp": map[string]any{}},
		"HostConfig": map[string]any{
			"PortBindings": map[string]any{
				"3000/tcp": []map[string]string{{"HostPort": strconv.Itoa(hostPort)}},
			},
		},
	}
	code, b, err := c.do("POST", "/containers/create?name="+name, body)
	if err != nil {
		return "", err
	}
	if code >= 400 {
		return "", fmt.Errorf("docker create container: HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	var out struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.ID == "" {
		return "", fmt.Errorf("docker create container: bad response %q", string(b))
	}
	return out.ID, nil
}

func (c *Client) start(containerID string) error {
	code, b, err := c.do("POST", "/containers/"+containerID+"/start", nil)
	if err != nil {
		return err
	}
	if code >= 400 {
		return fmt.Errorf("docker start container: HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	return nil
}

// Stop stops a container. A missing container is already stopped, so
// 404 is not an error here.
func (c *Client) Stop(containerID string) error {
	if containerID == "" {
		return nil
	}
	code, b, err := c.do("POST", "/containers/"+containerID+"/stop?t=5", nil)
	if err != nil {
		return err
	}
	if code == 404 {
		return nil
	}
	if code >= 400 {
		return fmt.Errorf("docker stop container: HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	return nil
}

// Remove force-removes a container. A missing container is not an error.
func (c *Client) Remove(containerID string) error {
	if containerID == "" {
		return nil
	}
	code, b, err := c.do("DELETE", "/containers/"+containerID+"?force=true", nil)
	if err != nil {
		return err
	}
	if code == 404 {
		return nil
	}
	if code >= 400 {
		return fmt.Errorf("docker remove container: HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	return nil
}

// InspectStatus returns "running", "stopped", or "not-found".
func (c *Client) InspectStatus(containerID string) (string, error) {
	if containerID == "" {
		return "not-found", nil
	}
	code, b, err := c.do("GET", "/containers/"+containerID+"/json", nil)
	if err != nil {
		return "", err
	}
	if code == 404 {
		return "not-found", nil
	}
	if code >= 400 {
		return "", fmt.Errorf("docker inspect container: HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	var out struct {
		State struct {
			Status  string `json:"Status"`
			Running bool   `json:"Running"`
		} `json:"State"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", fmt.Errorf("docker inspect container: bad response: %w", err)
	}
	if out.State.Running || out.State.Status == "running" {
		return "running", nil
	}
	return "stopped", nil
}

// CheckURL performs a single GET with a short timeout; only a 2xx
// response counts as healthy. It never claims healthy without checking.
func CheckURL(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// HealthCheck implements Deployer.HealthCheck.
func (c *Client) HealthCheck(url string) bool { return CheckURL(url) }

var _ Deployer = (*Client)(nil)
