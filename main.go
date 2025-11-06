// +ko-build
package main

import (
	"bytes"
	"crypto/tls"
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

// ClientConfig holds each client’s details and cached address
type ClientConfig struct {
	MAC      string `json:"mac"`
	GroupID  string `json:"group_id"`
	LastIPv6 string `json:"last_ipv6"`
}

// Config holds client info (no longer needs host/API key)
type Config struct {
	Clients []ClientConfig `json:"clients"`
}

// UniFiClient represents the API client record
type UniFiClient struct {
	MAC           string   `json:"mac"`
	IPv6Addresses []string `json:"ipv6_addresses"`
}

// ---- Helpers ----

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func saveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func makeRequest(method, url, apiKey string, body []byte, verifySSL bool) ([]byte, error) {
	req, err := http.NewRequest(method, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-KEY", apiKey)
	req.Header.Set("Content-Type", "application/json")

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifySSL},
	}
	client := &http.Client{Transport: tr}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

func getClients(host, apiKey string, verifySSL bool) ([]UniFiClient, error) {
	url := fmt.Sprintf("%s/proxy/network/api/s/default/stat/sta", host)
	data, err := makeRequest("GET", url, apiKey, nil, verifySSL)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data []UniFiClient `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func getGlobalIPv6(addresses []string) (string, error) {
	for _, ip := range addresses {
		ip = strings.TrimSpace(ip)
		if strings.HasPrefix(ip, "fe80") || strings.HasPrefix(ip, "FE80") {
			continue
		}
		if net.ParseIP(ip) != nil && strings.Contains(ip, ":") {
			return ip, nil
		}
	}
	return "", errors.New("no valid global IPv6 found")
}

func updateFirewallGroup(host, apiKey, groupID, newIPv6 string, verifySSL bool) error {
	url := fmt.Sprintf("%s/proxy/network/api/s/default/rest/firewallgroup/%s", host, groupID)
	payload := map[string]interface{}{
		"group_members": []string{newIPv6},
	}
	body, _ := json.Marshal(payload)

	_, err := makeRequest("PUT", url, apiKey, body, verifySSL)
	return err
}

// ---- Updater ----
func runUpdater(unifiHost, apiKey string, verifySSL bool, cfgPath string) {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		fmt.Println("❌ Failed to load config:", err)
		return
	}

	allClients, err := getClients(unifiHost, apiKey, verifySSL)
	if err != nil {
		fmt.Println("❌ Failed to get UniFi clients:", err)
		return
	}

	for i, c := range cfg.Clients {
		// Find client by MAC
		var found *UniFiClient
		for _, uc := range allClients {
			if strings.EqualFold(uc.MAC, c.MAC) {
				found = &uc
				break
			}
		}
		if found == nil {
			fmt.Println("⚠️  Client not found:", c.MAC)
			continue
		}

		// Pick global IPv6
		ipv6, err := getGlobalIPv6(found.IPv6Addresses)
		if err != nil {
			fmt.Printf("⚠️  No global IPv6 for %s (%v)\n", c.MAC, err)
			continue
		}

		if ipv6 != c.LastIPv6 {
			fmt.Printf("🔄 IPv6 changed for %s: %s → %s\n", c.MAC, c.LastIPv6, ipv6)
			if err := updateFirewallGroup(unifiHost, apiKey, c.GroupID, ipv6, verifySSL); err != nil {
				fmt.Println("❌ Failed to update firewall group:", err)
				continue
			}
			cfg.Clients[i].LastIPv6 = ipv6
			if err := saveConfig(cfgPath, cfg); err != nil {
				fmt.Println("❌ Failed to save config:", err)
			} else {
				fmt.Println("✅ Updated firewall group and saved new address.")
			}
		} else {
			fmt.Printf("✅ IPv6 unchanged for %s (%s)\n", c.MAC, ipv6)
		}
	}
}

// ---- Main ----
func main() {
	unifiHost := os.Getenv("UNIFI_HOST")
	apiKey := os.Getenv("UNIFI_API_KEY")
	cfgPath := "/app/clients.json"
	if cfgPathValue := os.Getenv("CONFIG_PATH"); cfgPathValue != "" {
		cfgPath = cfgPathValue
	}

	verifySSL := true
	if v := os.Getenv("VERIFY_SSL"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			verifySSL = parsed
		}
	}

	if unifiHost == "" || apiKey == "" {
		fmt.Println("❌ UNIFI_HOST and UNIFI_API_KEY environment variables are required")
		return
	}

	// Interval in seconds (default 3600 = 1h)
	interval := time.Hour
	if v := os.Getenv("CHECK_INTERVAL"); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			interval = time.Duration(seconds) * time.Second
		} else {
			fmt.Println("⚠️  Invalid CHECK_INTERVAL, using default 1h")
		}
	}

	fmt.Printf("✅ Running updater every %v\n", interval)

	// Run once immediately
	runUpdater(unifiHost, apiKey, verifySSL, cfgPath)

	// Schedule interval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		runUpdater(unifiHost, apiKey, verifySSL, cfgPath)
	}
}
