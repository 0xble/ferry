package share

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
)

func LocalTailscaleIPv4() (string, error) {
	out, err := exec.Command("tailscale", "ip", "-4").Output()
	if err != nil {
		return "", fmt.Errorf("tailscale ip -4: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("tailscale ip -4 returned no addresses")
	}
	return strings.TrimSpace(fields[0]), nil
}

type tailscaleStatus struct {
	Self struct {
		DNSName string `json:"DNSName"`
	} `json:"Self"`
}

type tailscaleServeStatus struct {
	Web map[string]tailscaleServeWeb `json:"Web"`
}

type tailscaleServeWeb struct {
	Handlers map[string]tailscaleServeHandler `json:"Handlers"`
}

type tailscaleServeHandler struct {
	Proxy string `json:"Proxy"`
}

func LocalTailscaleMagicDNS() (string, error) {
	out, err := exec.Command("tailscale", "status", "--json").Output()
	if err != nil {
		return "", fmt.Errorf("tailscale status --json: %w", err)
	}

	var status tailscaleStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return "", fmt.Errorf("decode tailscale status json: %w", err)
	}

	dnsName := strings.TrimSpace(status.Self.DNSName)
	dnsName = strings.TrimSuffix(dnsName, ".")
	if dnsName == "" {
		return "", fmt.Errorf("tailscale status --json missing Self.DNSName")
	}
	return dnsName, nil
}

func ExternalShareBaseURL(publicPort int) (string, error) {
	out, err := exec.Command("tailscale", "serve", "status", "--json").Output()
	if err != nil {
		return "", fmt.Errorf("tailscale serve status --json: %w", err)
	}

	var status tailscaleServeStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return "", fmt.Errorf("decode tailscale serve status json: %w", err)
	}

	for host, web := range status.Web {
		for path, handler := range web.Handlers {
			if path != "/share" && path != "/share/" {
				continue
			}
			if !shareProxyMatchesPort(handler.Proxy, publicPort) {
				continue
			}
			trimmedHost := strings.TrimSpace(host)
			trimmedHost = strings.TrimSuffix(trimmedHost, ":443")
			return fmt.Sprintf("https://%s/share", trimmedHost), nil
		}
	}

	return "", fmt.Errorf("tailscale serve missing /share route for port %d", publicPort)
}

func shareProxyMatchesPort(proxy string, publicPort int) bool {
	raw := strings.TrimSpace(proxy)
	if raw == "" {
		return false
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}

	port := parsed.Port()
	if port == "" {
		return false
	}

	value, err := strconv.Atoi(port)
	if err != nil {
		return false
	}

	return value == publicPort
}
