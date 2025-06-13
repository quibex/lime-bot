package config

import "os"

type Config struct {
	// Telegram
	BotToken         string
	SuperAdminID     string
	ReviewsChannelID string

	// БД
	DBDsn string

	// wg-agent gRPC
	WGAgentAddr  string
	WGClientCert string
	WGClientKey  string
	WGCACert     string

	// Health-check
	TGToken  string
	TGChatID string
}

func Load() *Config {
	return &Config{
		BotToken:         os.Getenv("BOT_TOKEN"),
		SuperAdminID:     os.Getenv("SUPER_ADMIN_ID"),
		ReviewsChannelID: os.Getenv("REVIEWS_CHANNEL_ID"),

		DBDsn: getEnvOrDefault("DB_DSN", "file://data/limevpn.db"),

		WGAgentAddr:  getEnvOrDefault("WG_AGENT_ADDR", "wg-agent:7443"),
		WGClientCert: os.Getenv("WG_CLIENT_CERT"),
		WGClientKey:  os.Getenv("WG_CLIENT_KEY"),
		WGCACert:     os.Getenv("WG_CA_CERT"),

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
