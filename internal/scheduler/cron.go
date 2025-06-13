package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"lime-bot/internal/config"
	"lime-bot/internal/db"
	"lime-bot/internal/gates/wgagent"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron     *cron.Cron
	repo     *db.Repository
	bot      *tgbotapi.BotAPI
	cfg      *config.Config
	wgClient *wgagent.Client
}

func NewScheduler(repo *db.Repository, bot *tgbotapi.BotAPI, cfg *config.Config) (*Scheduler, error) {
	// Создаем wg-agent клиент
	wgConfig := wgagent.Config{
		Addr: cfg.WGAgentAddr,
	}
	wgClient, err := wgagent.NewClient(wgConfig)
	if err != nil {
		return nil, err
	}

	return &Scheduler{
		cron:     cron.New(),
		repo:     repo,
		bot:      bot,
		cfg:      cfg,
		wgClient: wgClient,
	}, nil
}

func (s *Scheduler) Start() error {
	// Cron-задача: отключение истекших подписок (ежедневно в 00:10)
	_, err := s.cron.AddFunc("10 0 * * *", s.disableExpiredSubscriptions)
	if err != nil {
		return fmt.Errorf("failed to add expired subscriptions job: %w", err)
	}

	// Cron-задача: напоминания об истечении (каждые 30 минут)
	_, err = s.cron.AddFunc("*/30 * * * *", s.sendExpirationReminders)
	if err != nil {
		return fmt.Errorf("failed to add expiration reminders job: %w", err)
	}

	// Cron-задача: health-check wg-agent (каждые 5 минут)
	_, err = s.cron.AddFunc("*/5 * * * *", s.healthCheckWGAgent)
	if err != nil {
		return fmt.Errorf("failed to add health check job: %w", err)
	}

	// Запускаем планировщик
	s.cron.Start()
	slog.Info("Cron scheduler started")

	return nil
}

func (s *Scheduler) Stop() {
	s.cron.Stop()
	s.wgClient.Close()
	slog.Info("Cron scheduler stopped")
}

// disableExpiredSubscriptions отключает все подписки с истекшим сроком
func (s *Scheduler) disableExpiredSubscriptions() {
	slog.Info("Running expired subscriptions cleanup...")

	// Получаем все активные подписки с истекшим сроком
	var expiredSubs []db.Subscription
	today := time.Now().Format("2006-01-02")

	result := s.repo.DB().Where("active = true AND end_date < ?", today).Find(&expiredSubs)
	if result.Error != nil {
		slog.Error("Error fetching expired subscriptions", "error", result.Error)
		return
	}

	if len(expiredSubs) == 0 {
		slog.Info("No expired subscriptions found")
		return
	}

	disabled := 0
	removed := 0
	ctx := context.Background()

	for _, sub := range expiredSubs {
		// Сначала отключаем peer
		disableReq := &wgagent.DisablePeerRequest{
			Interface: sub.Interface,
			PublicKey: sub.PublicKey,
		}

		err := s.wgClient.DisablePeer(ctx, disableReq)
		if err != nil {
			slog.Error("Failed to disable peer", "peer_id", sub.PeerID, "error", err)
			continue
		}

		// Затем удаляем peer (для полной очистки)
		removeReq := &wgagent.RemovePeerRequest{
			Interface: sub.Interface,
			PublicKey: sub.PublicKey,
		}

		err = s.wgClient.RemovePeer(ctx, removeReq)
		if err != nil {
			slog.Error("Failed to remove peer", "peer_id", sub.PeerID, "error", err)
			disabled++
		} else {
			removed++
		}

		// Обновляем статус в БД
		s.repo.DB().Model(&sub).Update("active", false)
	}

	slog.Info("Expired subscriptions cleanup completed", "disabled", disabled, "removed", removed)

	// Отправляем отчет супер-админу
	s.sendAdminReport(fmt.Sprintf("🕒 Автоматическая очистка:\n✅ Отключено: %d\n🗑 Удалено: %d", disabled, removed))
}

// sendExpirationReminders отправляет напоминания о скором истечении подписок
func (s *Scheduler) sendExpirationReminders() {
	slog.Info("Checking for expiration reminders...")

	// Получаем подписки, которые истекают через 3 дня
	threeDaysLater := time.Now().AddDate(0, 0, 3).Format("2006-01-02")

	var soonExpiringSubs []db.Subscription
	result := s.repo.DB().Where("active = true AND end_date = ?", threeDaysLater).
		Preload("User").
		Preload("Plan").
		Find(&soonExpiringSubs)

	if result.Error != nil {
		slog.Error("Error fetching soon expiring subscriptions", "error", result.Error)
		return
	}

	if len(soonExpiringSubs) == 0 {
		return // Нет подписок, которые скоро истекают
	}

	slog.Info("Found subscriptions expiring in 3 days", "count", len(soonExpiringSubs))

	for _, sub := range soonExpiringSubs {
		text := fmt.Sprintf(`⚠️ Напоминание о подписке

Ваша подписка "%s" истекает через 3 дня (%s).

Не забудьте продлить подписку, чтобы не потерять доступ к VPN!

Для продления используйте команду /buy`,
			sub.Plan.Name,
			sub.EndDate.Format("02.01.2006"),
		)

		msg := tgbotapi.NewMessage(sub.User.TgID, text)
		_, err := s.bot.Send(msg)
		if err != nil {
			slog.Error("Failed to send expiration reminder", "user_id", sub.User.TgID, "error", err)
		}
	}
}

// healthCheckWGAgent проверяет состояние wg-agent
func (s *Scheduler) healthCheckWGAgent() {
	// Простая проверка доступности wg-agent

	// Пытаемся создать клиент
	wgConfig := wgagent.Config{
		Addr: s.cfg.WGAgentAddr,
	}

	testClient, err := wgagent.NewClient(wgConfig)
	if err != nil {
		s.sendHealthAlert(fmt.Sprintf("❌ WG-Agent недоступен: %v", err))
		return
	}
	defer testClient.Close()

	slog.Info("WG-Agent health check passed")
}

// sendAdminReport отправляет отчет супер-админу
func (s *Scheduler) sendAdminReport(message string) {
	if s.cfg.SuperAdminID == "" {
		return
	}

	adminID, err := strconv.ParseInt(s.cfg.SuperAdminID, 10, 64)
	if err != nil {
		return
	}

	msg := tgbotapi.NewMessage(adminID, message)
	s.bot.Send(msg)
}

// sendHealthAlert отправляет алерт о проблемах со здоровьем системы
func (s *Scheduler) sendHealthAlert(message string) {
	slog.Warn("Health alert", "message", message)
	s.sendAdminReport("🚨 " + message)
}
