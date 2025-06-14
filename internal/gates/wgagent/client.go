package wgagent

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
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

// Client клиент для работы с WG агентом
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
	// Создаем HTTP клиент с TLS конфигурацией
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Если указаны сертификаты, настраиваем TLS
	if cfg.CertFile != "" && cfg.KeyFile != "" && cfg.CAFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("ошибка загрузки клиентского сертификата: %w", err)
		}

		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("ошибка чтения CA сертификата: %w", err)
		}

		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      caCertPool,
		}

		httpClient.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
	} else {
		// Для разработки - игнорируем валидацию сертификатов
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	return &Client{
		addr:       cfg.Addr,
		httpClient: httpClient,
	}, nil
}

// Close закрывает клиент
func (c *Client) Close() error {
	return nil
}

// makeRequest выполняет HTTP запрос к WG агенту
func (c *Client) makeRequest(ctx context.Context, method, endpoint string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("ошибка сериализации JSON: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	url := fmt.Sprintf("https://%s%s", c.addr, endpoint)
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}

	return resp, nil
}

// GeneratePeerConfig генерирует конфигурацию для нового пира
func (c *Client) GeneratePeerConfig(ctx context.Context, req *GeneratePeerConfigRequest) (*GeneratePeerConfigResponse, error) {
	resp, err := c.makeRequest(ctx, "POST", "/api/v1/peers/generate", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("WG агент вернул ошибку %d: %s", resp.StatusCode, string(body))
	}

	var result GeneratePeerConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ошибка десериализации ответа: %w", err)
	}

	return &result, nil
}

// AddPeer добавляет пира к интерфейсу
func (c *Client) AddPeer(ctx context.Context, req *AddPeerRequest) (*AddPeerResponse, error) {
	resp, err := c.makeRequest(ctx, "POST", "/api/v1/peers", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("WG агент вернул ошибку %d: %s", resp.StatusCode, string(body))
	}

	var result AddPeerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ошибка десериализации ответа: %w", err)
	}

	return &result, nil
}

// RemovePeer удаляет пира
func (c *Client) RemovePeer(ctx context.Context, req *RemovePeerRequest) error {
	resp, err := c.makeRequest(ctx, "DELETE", "/api/v1/peers", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("WG агент вернул ошибку %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// DisablePeer отключает пира
func (c *Client) DisablePeer(ctx context.Context, req *DisablePeerRequest) error {
	resp, err := c.makeRequest(ctx, "PUT", "/api/v1/peers/disable", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("WG агент вернул ошибку %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// EnablePeer включает пира
func (c *Client) EnablePeer(ctx context.Context, req *EnablePeerRequest) error {
	resp, err := c.makeRequest(ctx, "PUT", "/api/v1/peers/enable", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("WG агент вернул ошибку %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
