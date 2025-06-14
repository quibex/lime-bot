package config

import "os"

type Config struct {
	BotToken         string
	SuperAdminID     string
	ReviewsChannelID string

	DBDsn string

	WGAgentAddr      string
	WGClientCert     string
	WGClientKey      string
	WGCACert         string
	WGServerEndpoint string

	HealthAddr string

	TGToken  string
	TGChatID string
}

func Load() *Config {
	return &Config{
		BotToken:         os.Getenv("BOT_TOKEN"),
		SuperAdminID:     os.Getenv("SUPER_ADMIN_ID"),
		ReviewsChannelID: os.Getenv("REVIEWS_CHANNEL_ID"),

		DBDsn: getEnvOrDefault("DB_DSN", "/data/limevpn.db"),

		WGAgentAddr:      getEnvOrDefault("WG_AGENT_ADDR", "wg-agent:7443"),
		WGClientCert:     os.Getenv("WG_CLIENT_CERT"),
		WGClientKey:      os.Getenv("WG_CLIENT_KEY"),
		WGCACert:         os.Getenv("WG_CA_CERT"),
		WGServerEndpoint: getEnvOrDefault("WG_SERVER_ENDPOINT", "vpn.example.com:51820"),

		HealthAddr: getEnvOrDefault("HEALTH_ADDR", "0.0.0.0:8080"),

		TGToken:  os.Getenv("TG_TOKEN"),
		TGChatID: os.Getenv("TG_CHAT_ID"),
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
