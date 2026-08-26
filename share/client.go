package share

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	http       *http.Client
	publicHTTP *http.Client
}

type healthResponse struct {
	OK            bool   `json:"ok"`
	PublicBaseURL string `json:"public_base_url"`
	HealthBaseURL string `json:"health_base_url"`
}

// NewClient builds an admin client. A path-shaped address (or a "unix:"
// prefix) dials a Unix domain socket; otherwise the address is used as a TCP
// host:port. UDS is the production transport, TCP is reserved for tests.
//
// The returned client carries a separate http.Client for probing the
// public-facing health URL. Without that, the UDS transport would also route
// the public probe to the admin socket because DialContext ignores the
// requested address.
func NewClient(adminAddr string) *Client {
	network, target := parseAdminAddr(strings.TrimSpace(adminAddr))
	publicHTTP := &http.Client{
		Timeout: 3 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	if network == "unix" {
		socketPath := target
		adminHTTP := &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
		}
		return &Client{
			baseURL:    "http://ferry-admin",
			http:       adminHTTP,
			publicHTTP: publicHTTP,
		}
	}

	base := target
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return &Client{
		baseURL:    strings.TrimRight(base, "/"),
		http:       &http.Client{Timeout: 3 * time.Second},
		publicHTTP: publicHTTP,
	}
}

func (c *Client) Health() error {
	resp, err := c.http.Get(c.baseURL + "/admin/health")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}

	var report healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return fmt.Errorf("decode admin health: %w", err)
	}
	if !report.OK {
		return fmt.Errorf("admin health not ok")
	}
	healthBase := strings.TrimSpace(report.HealthBaseURL)
	requireLoopback := healthBase != ""
	if healthBase == "" {
		healthBase = strings.TrimSpace(report.PublicBaseURL)
	}
	if healthBase == "" {
		return fmt.Errorf("admin health missing public base url")
	}
	healthURL, err := buildHealthURL(healthBase, requireLoopback)
	if err != nil {
		return err
	}
	return c.probePublicHealth(healthURL)
}

func buildHealthURL(baseURL string, requireLoopback bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("invalid health base url: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("invalid health base url")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("invalid health base url")
	}
	if requireLoopback {
		ip := net.ParseIP(u.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return "", fmt.Errorf("health base url must use a loopback IP address")
		}
	}
	return strings.TrimRight(u.String(), "/") + "/healthz", nil
}

func (c *Client) probePublicHealth(healthURL string) error {
	publicResp, err := c.publicHTTP.Get(healthURL)
	if err != nil {
		return fmt.Errorf("public health request failed: %w", err)
	}
	defer func() { _ = publicResp.Body.Close() }()
	if publicResp.StatusCode != http.StatusOK {
		return fmt.Errorf("public health status %d", publicResp.StatusCode)
	}

	return nil
}

func (c *Client) CreateShare(req CreateShareRequest) (ShareResponse, error) {
	var out ShareResponse
	if req.Mode == "" {
		req.Mode = ModeLive
	}
	body, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	resp, err := c.http.Post(c.baseURL+"/admin/share", "application/json", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return out, decodeAPIError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) ListShares() ([]ShareResponse, error) {
	resp, err := c.http.Get(c.baseURL + "/admin/shares")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, decodeAPIError(resp)
	}
	var out []ShareResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetShare(id string) (ShareResponse, error) {
	var out ShareResponse
	resp, err := c.http.Get(c.baseURL + "/admin/shares/" + id)
	if err != nil {
		return out, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return out, decodeAPIError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) RevokeShare(id string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+"/admin/shares/"+id, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return decodeAPIError(resp)
	}
	return nil
}

func (c *Client) RenewShare(id string, expiresIn time.Duration) (ShareResponse, error) {
	var out ShareResponse
	reqBody := RenewShareRequest{ExpiresInSeconds: int64(expiresIn / time.Second)}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return out, err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/admin/shares/"+id+"/renew", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return out, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return out, decodeAPIError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

func decodeAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		return fmt.Errorf("request failed: status %d", resp.StatusCode)
	}
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error.Message != "" {
		return fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
	}
	return fmt.Errorf("request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}
