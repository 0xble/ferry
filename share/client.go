package share

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

type healthResponse struct {
	OK            bool   `json:"ok"`
	PublicBaseURL string `json:"public_base_url"`
}

func NewClient(adminAddr string) *Client {
	base := strings.TrimSpace(adminAddr)
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return &Client{
		baseURL: strings.TrimRight(base, "/"),
		http: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

func (c *Client) Health() error {
	resp, err := c.http.Get(c.baseURL + "/admin/health")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("health status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var report healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return fmt.Errorf("decode admin health: %w", err)
	}
	if !report.OK {
		return fmt.Errorf("admin health not ok")
	}
	publicBase := strings.TrimSpace(report.PublicBaseURL)
	if publicBase == "" {
		return fmt.Errorf("admin health missing public base url")
	}

	publicURL := strings.TrimRight(publicBase, "/") + "/healthz"
	publicResp, err := c.http.Get(publicURL)
	if err != nil {
		return fmt.Errorf("public health request failed: %w", err)
	}
	defer func() { _ = publicResp.Body.Close() }()
	if publicResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(publicResp.Body)
		return fmt.Errorf("public health status %d: %s", publicResp.StatusCode, strings.TrimSpace(string(body)))
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
