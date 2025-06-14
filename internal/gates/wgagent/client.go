package wgagent

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// GeneratePeerConfigRequest запрос на генерацию конфигурации пира
type GeneratePeerConfigRequest struct {
	Interface      string `json:"interface"`
	ServerEndpoint string `json:"server_endpoint"`
	DNSServers     string `json:"dns_servers"`
	AllowedIPs     string `json:"allowed_ips"`
}

// GeneratePeerConfigResponse ответ с конфигурацией пира
type GeneratePeerConfigResponse struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	Config     string `json:"config"`
	QRCode     string `json:"qr_code"`
	AllowedIP  string `json:"allowed_ip"`
}

// AddPeerRequest запрос на добавление пира
type AddPeerRequest struct {
	Interface  string `json:"interface"`
	PublicKey  string `json:"public_key"`
	AllowedIP  string `json:"allowed_ip"`
	KeepaliveS int32  `json:"keepalive_s"`
	PeerID     string `json:"peer_id"`
}

// AddPeerResponse ответ на добавление пира
type AddPeerResponse struct {
	ListenPort int32  `json:"listen_port"`
	Config     string `json:"config"`
	QRCode     string `json:"qr_code"`
}

// DisablePeerRequest запрос на отключение пира
type DisablePeerRequest struct {
	Interface string `json:"interface"`
	PublicKey string `json:"public_key"`
}

// EnablePeerRequest запрос на включение пира
type EnablePeerRequest struct {
	Interface string `json:"interface"`
	PublicKey string `json:"public_key"`
}

// RemovePeerRequest запрос на удаление пира
type RemovePeerRequest struct {
	Interface string `json:"interface"`
	PublicKey string `json:"public_key"`
}

// Client представляет клиент для взаимодействия с WG агентом
type Client struct {
	addr       string
	httpClient *http.Client
}

// Config конфигурация клиента
type Config struct {
	Addr     string
	CertFile string
	KeyFile  string
	CAFile   string
}

// NewClient создает новый клиент WG агента
func NewClient(cfg Config) (*Client, error) {
	slog.Info("Creating WG Agent client", "addr", cfg.Addr, "has_certs", cfg.CertFile != "")

	// Создаем HTTP клиент с TLS конфигурацией
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Если указаны сертификаты, настраиваем TLS
	if cfg.CertFile != "" && cfg.KeyFile != "" && cfg.CAFile != "" {
		slog.Info("Loading TLS certificates for WG Agent", "cert_file", cfg.CertFile, "ca_file", cfg.CAFile)

		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			slog.Error("Failed to load client certificate", "cert_file", cfg.CertFile, "key_file", cfg.KeyFile, "error", err)
			return nil, errors.New("failed to load client certificate: " + err.Error())
		}

		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			slog.Error("Failed to read CA certificate", "ca_file", cfg.CAFile, "error", err)
			return nil, errors.New("failed to read CA certificate: " + err.Error())
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			slog.Error("Failed to parse CA certificate", "ca_file", cfg.CAFile)
			return nil, errors.New("failed to parse CA certificate")
		}

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      caCertPool,
		}

		httpClient.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}

		slog.Info("TLS configuration loaded successfully")
	} else {
		// Для разработки - игнорируем валидацию сертификатов
		slog.Warn("Using insecure TLS configuration (certificates not provided)")
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	slog.Info("WG Agent client created successfully", "addr", cfg.Addr)

	return &Client{
		addr:       cfg.Addr,
		httpClient: httpClient,
	}, nil
}

// Close закрывает клиент
func (c *Client) Close() error {
	slog.Debug("Closing WG Agent client")
	return nil
}

// makeRequest выполняет HTTP запрос к WG агенту
func (c *Client) makeRequest(ctx context.Context, method, endpoint string, body interface{}) (*http.Response, error) {
	slog.Debug("Making WG Agent request", "method", method, "endpoint", endpoint, "has_body", body != nil)

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			slog.Error("Failed to marshal request body", "error", err)
			return nil, errors.New("failed to marshal request body: " + err.Error())
		}
		reqBody = bytes.NewBuffer(jsonData)
		slog.Debug("Request body marshaled", "size", len(jsonData))
	}

	url := "https://" + c.addr + endpoint
	slog.Debug("Creating HTTP request", "url", url)

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		slog.Error("Failed to create HTTP request", "url", url, "error", err)
		return nil, errors.New("failed to create HTTP request: " + err.Error())
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	slog.Debug("Executing HTTP request", "url", url, "method", method)
	start := time.Now()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		duration := time.Since(start)
		slog.Error("HTTP request failed", "url", url, "duration", duration, "error", err)
		return nil, errors.New("HTTP request failed: " + err.Error())
	}

	duration := time.Since(start)
	slog.Debug("HTTP request completed", "url", url, "status", resp.StatusCode, "duration", duration)

	return resp, nil
}

// GeneratePeerConfig генерирует конфигурацию для нового пира
func (c *Client) GeneratePeerConfig(ctx context.Context, req *GeneratePeerConfigRequest) (*GeneratePeerConfigResponse, error) {
	slog.Info("Generating peer config", "interface", req.Interface, "server_endpoint", req.ServerEndpoint)

	resp, err := c.makeRequest(ctx, "POST", "/api/v1/peers/generate", req)
	if err != nil {
		slog.Error("Failed to generate peer config", "error", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("WG Agent returned error for peer generation", "status", resp.StatusCode, "body", string(body))
		return nil, errors.New("WG agent error " + string(rune(resp.StatusCode)) + ": " + string(body))
	}

	var result GeneratePeerConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("Failed to decode peer config response", "error", err)
		return nil, errors.New("failed to decode response: " + err.Error())
	}

	slog.Info("Peer config generated successfully", "public_key", result.PublicKey[:10]+"...", "allowed_ip", result.AllowedIP)
	return &result, nil
}

// AddPeer добавляет пира к интерфейсу
func (c *Client) AddPeer(ctx context.Context, req *AddPeerRequest) (*AddPeerResponse, error) {
	slog.Info("Adding peer to interface", "interface", req.Interface, "peer_id", req.PeerID, "allowed_ip", req.AllowedIP)

	resp, err := c.makeRequest(ctx, "POST", "/api/v1/peers", req)
	if err != nil {
		slog.Error("Failed to add peer", "peer_id", req.PeerID, "error", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("WG Agent returned error for peer addition", "status", resp.StatusCode, "body", string(body), "peer_id", req.PeerID)
		return nil, errors.New("WG agent error " + string(rune(resp.StatusCode)) + ": " + string(body))
	}

	var result AddPeerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("Failed to decode add peer response", "peer_id", req.PeerID, "error", err)
		return nil, errors.New("failed to decode response: " + err.Error())
	}

	slog.Info("Peer added successfully", "peer_id", req.PeerID, "interface", req.Interface)
	return &result, nil
}

// RemovePeer удаляет пира
func (c *Client) RemovePeer(ctx context.Context, req *RemovePeerRequest) error {
	slog.Info("Removing peer", "interface", req.Interface, "public_key", req.PublicKey[:10]+"...")

	resp, err := c.makeRequest(ctx, "DELETE", "/api/v1/peers", req)
	if err != nil {
		slog.Error("Failed to remove peer", "public_key", req.PublicKey[:10]+"...", "error", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("WG Agent returned error for peer removal", "status", resp.StatusCode, "body", string(body), "public_key", req.PublicKey[:10]+"...")
		return errors.New("WG agent error " + string(rune(resp.StatusCode)) + ": " + string(body))
	}

	slog.Info("Peer removed successfully", "public_key", req.PublicKey[:10]+"...")
	return nil
}

// DisablePeer отключает пира
func (c *Client) DisablePeer(ctx context.Context, req *DisablePeerRequest) error {
	slog.Info("Disabling peer", "interface", req.Interface, "public_key", req.PublicKey[:10]+"...")

	resp, err := c.makeRequest(ctx, "PUT", "/api/v1/peers/disable", req)
	if err != nil {
		slog.Error("Failed to disable peer", "public_key", req.PublicKey[:10]+"...", "error", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("WG Agent returned error for peer disable", "status", resp.StatusCode, "body", string(body), "public_key", req.PublicKey[:10]+"...")
		return errors.New("WG agent error " + string(rune(resp.StatusCode)) + ": " + string(body))
	}

	slog.Info("Peer disabled successfully", "public_key", req.PublicKey[:10]+"...")
	return nil
}

// EnablePeer включает пира
func (c *Client) EnablePeer(ctx context.Context, req *EnablePeerRequest) error {
	slog.Info("Enabling peer", "interface", req.Interface, "public_key", req.PublicKey[:10]+"...")

	resp, err := c.makeRequest(ctx, "PUT", "/api/v1/peers/enable", req)
	if err != nil {
		slog.Error("Failed to enable peer", "public_key", req.PublicKey[:10]+"...", "error", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("WG Agent returned error for peer enable", "status", resp.StatusCode, "body", string(body), "public_key", req.PublicKey[:10]+"...")
		return errors.New("WG agent error " + string(rune(resp.StatusCode)) + ": " + string(body))
	}

	slog.Info("Peer enabled successfully", "public_key", req.PublicKey[:10]+"...")
	return nil
}
