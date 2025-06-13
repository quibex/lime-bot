package wgagent

import (
	"context"
	"fmt"
)


type GeneratePeerConfigRequest struct {
	Interface      string
	ServerEndpoint string
	DNSServers     string
	AllowedIPs     string
}

type GeneratePeerConfigResponse struct {
	PrivateKey string
	PublicKey  string
	Config     string
	QRCode     string
	AllowedIP  string
}

type AddPeerRequest struct {
	Interface  string
	PublicKey  string
	AllowedIP  string
	KeepaliveS int32
	PeerID     string
}

type AddPeerResponse struct {
	ListenPort int32
	Config     string
	QRCode     string
}

type DisablePeerRequest struct {
	Interface string
	PublicKey string
}

type EnablePeerRequest struct {
	Interface string
	PublicKey string
}

type RemovePeerRequest struct {
	Interface string
	PublicKey string
}

type Client struct {
	addr string
}

type Config struct {
	Addr     string
	CertFile string
	KeyFile  string
	CAFile   string
}

func NewClient(cfg Config) (*Client, error) {
	return &Client{
		addr: cfg.Addr,
	}, nil
}

func (c *Client) Close() error {
	return nil
}

func (c *Client) GeneratePeerConfig(ctx context.Context, req *GeneratePeerConfigRequest) (*GeneratePeerConfigResponse, error) {
	
	return &GeneratePeerConfigResponse{
		PrivateKey: "mock_private_key",
		PublicKey:  "mock_public_key",
		Config:     fmt.Sprintf("mock_config_for_%s", req.Interface),
		QRCode:     "mock_qr_code_base64",
		AllowedIP:  "10.8.0.100/32",
	}, nil
}

func (c *Client) AddPeer(ctx context.Context, req *AddPeerRequest) (*AddPeerResponse, error) {
	
	return &AddPeerResponse{
		ListenPort: 51820,
		Config:     fmt.Sprintf("mock_config_for_peer_%s", req.PeerID),
		QRCode:     "mock_qr_code_base64",
	}, nil
}

func (c *Client) RemovePeer(ctx context.Context, req *RemovePeerRequest) error {
	
	return nil
}

func (c *Client) DisablePeer(ctx context.Context, req *DisablePeerRequest) error {
	
	return nil
}

func (c *Client) EnablePeer(ctx context.Context, req *EnablePeerRequest) error {
	
	return nil
}
