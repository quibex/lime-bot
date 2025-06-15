package wgtest

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"lime-bot/internal/gates/wgagent"
)

// IntegrationTest представляет тест подключения к WG Agent
type IntegrationTest struct {
	config   wgagent.Config
	notifyFn func(message string)
}

// NewIntegrationTest создает новый интеграционный тест
func NewIntegrationTest(config wgagent.Config, notifyFn func(string)) *IntegrationTest {
	return &IntegrationTest{
		config:   config,
		notifyFn: notifyFn,
	}
}

// RunStartupTest запускает тест подключения при старте приложения
func (it *IntegrationTest) RunStartupTest(ctx context.Context) error {
	slog.Info("Starting WG Agent integration test", "wg_addr", it.config.Addr)

	// Тест 1: Основное подключение
	if err := it.testConnection(ctx); err != nil {
		errorMsg := fmt.Sprintf("🚨 WG Agent недоступен при старте!\n\n❌ Ошибка: %v\n🌐 Адрес: %s\n\n⚠️ VPN ключи не смогут создаваться!", err, it.config.Addr)
		it.notifyFn(errorMsg)
		return err
	}

	// Тест 2: Проверка функций API
	if err := it.testAPIFunctions(ctx); err != nil {
		errorMsg := fmt.Sprintf("⚠️ WG Agent подключен, но API работает некорректно!\n\n❌ Ошибка: %v\n🌐 Адрес: %s", err, it.config.Addr)
		it.notifyFn(errorMsg)
		return err
	}

	slog.Info("WG Agent integration test passed successfully")
	successMsg := fmt.Sprintf("✅ WG Agent подключен успешно!\n\n🌐 Адрес: %s\n🔧 Все функции API работают корректно", it.config.Addr)
	it.notifyFn(successMsg)
	return nil
}

// testConnection проверяет базовое подключение
func (it *IntegrationTest) testConnection(ctx context.Context) error {
	slog.Info("Testing WG Agent connection")

	client, err := wgagent.NewClient(it.config)
	if err != nil {
		slog.Error("Failed to create WG Agent client", "error", err)
		return fmt.Errorf("создание клиента: %w", err)
	}
	defer client.Close()

	// Устанавливаем таймаут для теста
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Проверяем что можем подключиться и получить ответ
	req := &wgagent.GeneratePeerConfigRequest{
		Interface:      "wg0",
		ServerEndpoint: "test.example.com:51820",
		DNSServers:     "1.1.1.1",
		AllowedIPs:     "0.0.0.0/0",
	}

	_, err = client.GeneratePeerConfig(testCtx, req)
	if err != nil {
		slog.Error("WG Agent connection test failed", "error", err)
		return fmt.Errorf("тест подключения: %w", err)
	}

	slog.Info("WG Agent connection test passed")
	return nil
}

// testAPIFunctions проверяет основные функции API
func (it *IntegrationTest) testAPIFunctions(ctx context.Context) error {
	slog.Info("Testing WG Agent API functions")

	client, err := wgagent.NewClient(it.config)
	if err != nil {
		return fmt.Errorf("создание клиента: %w", err)
	}
	defer client.Close()

	testCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Тест 1: Генерация конфигурации peer
	slog.Info("Testing peer config generation")
	peerReq := &wgagent.GeneratePeerConfigRequest{
		Interface:      "wg0",
		ServerEndpoint: "test.example.com:51820",
		DNSServers:     "1.1.1.1, 1.0.0.1",
		AllowedIPs:     "0.0.0.0/0",
	}

	peerResp, err := client.GeneratePeerConfig(testCtx, peerReq)
	if err != nil {
		slog.Error("Peer config generation test failed", "error", err)
		return fmt.Errorf("генерация конфигурации peer: %w", err)
	}

	if peerResp.PrivateKey == "" || peerResp.PublicKey == "" {
		return fmt.Errorf("получены пустые ключи от WG Agent")
	}

	// Тест 2: Добавление peer
	slog.Info("Testing peer addition")
	testPeerID := fmt.Sprintf("test_peer_%d", time.Now().Unix())
	addReq := &wgagent.AddPeerRequest{
		Interface:  "wg0",
		PublicKey:  peerResp.PublicKey,
		AllowedIP:  peerResp.AllowedIP,
		KeepaliveS: 25,
		PeerID:     testPeerID,
	}

	_, err = client.AddPeer(testCtx, addReq)
	if err != nil {
		slog.Error("Peer addition test failed", "error", err)
		return fmt.Errorf("добавление peer: %w", err)
	}

	// Тест 3: Удаление тестового peer (очистка)
	slog.Info("Cleaning up test peer")
	removeReq := &wgagent.RemovePeerRequest{
		Interface: "wg0",
		PublicKey: peerResp.PublicKey,
	}

	err = client.RemovePeer(testCtx, removeReq)
	if err != nil {
		slog.Warn("Failed to cleanup test peer", "error", err, "peer_id", testPeerID)
		// Не возвращаем ошибку, так как это не критично
	}

	slog.Info("WG Agent API functions test passed")
	return nil
}

// RunPeriodicHealthCheck запускает периодическую проверку здоровья WG Agent
func (it *IntegrationTest) RunPeriodicHealthCheck(ctx context.Context, interval time.Duration) {
	slog.Info("Starting periodic WG Agent health check", "interval", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	consecutiveFailures := 0
	const maxFailures = 3

	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping WG Agent health check")
			return
		case <-ticker.C:
			if err := it.testConnection(ctx); err != nil {
				consecutiveFailures++
				slog.Error("WG Agent health check failed", "error", err, "consecutive_failures", consecutiveFailures)

				if consecutiveFailures >= maxFailures {
					criticalMsg := fmt.Sprintf("🚨 КРИТИЧЕСКАЯ ОШИБКА: WG Agent недоступен уже %d раз подряд!\n\n❌ Ошибка: %v\n🌐 Адрес: %s\n\n⚠️ Немедленно проверьте сервис!", consecutiveFailures, err, it.config.Addr)
					it.notifyFn(criticalMsg)
					consecutiveFailures = 0 // Сбрасываем счетчик чтобы не спамить
				}
			} else {
				if consecutiveFailures > 0 {
					slog.Info("WG Agent health check recovered", "after_failures", consecutiveFailures)
					recoveryMsg := fmt.Sprintf("✅ WG Agent восстановлен после %d неудачных попыток\n\n🌐 Адрес: %s\n🔧 Сервис снова работает корректно", consecutiveFailures, it.config.Addr)
					it.notifyFn(recoveryMsg)
				}
				consecutiveFailures = 0
			}
		}
	}
}
