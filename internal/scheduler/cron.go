package scheduler

import (
	"context"
	"errors"
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
	slog.Info("Creating scheduler", "wg_addr", cfg.WGAgentAddr)

	// Создаем WG клиент с настройками
	wgConfig := wgagent.Config{
		Addr:     cfg.WGAgentAddr,
		CertFile: cfg.WGClientCert,
		KeyFile:  cfg.WGClientKey,
		CAFile:   cfg.WGCACert,
	}

	if cfg.WGClientCert == "" || cfg.WGClientKey == "" || cfg.WGCACert == "" {
		slog.Warn("WG certificates not configured in scheduler, using insecure connection")
		wgConfig = wgagent.Config{
			Addr: cfg.WGAgentAddr,
		}
	}

	wgClient, err := wgagent.NewClient(wgConfig)
	if err != nil {
		slog.Error("Failed to create WG client for scheduler", "error", err, "wg_addr", cfg.WGAgentAddr)
		return nil, errors.New("failed to create WG client: " + err.Error())
	}

	slog.Info("Scheduler WG client created successfully")

	return &Scheduler{
		cron:     cron.New(),
		repo:     repo,
		bot:      bot,
		cfg:      cfg,
		wgClient: wgClient,
	}, nil
}

func (s *Scheduler) Start() error {
	slog.Info("Starting scheduler with cron jobs")

	// Отключение просроченных подписок - каждый день в 00:10
	_, err := s.cron.AddFunc("10 0 * * *", s.disableExpiredSubscriptions)
	if err != nil {
		return errors.New("failed to add expired subscriptions job: " + err.Error())
	}
	slog.Info("Added expired subscriptions cleanup job: daily at 00:10")

	// Напоминания об истечении - каждые 30 минут
	_, err = s.cron.AddFunc("*/30 * * * *", s.sendExpirationReminders)
	if err != nil {
		return errors.New("failed to add expiration reminders job: " + err.Error())
	}
	slog.Info("Added expiration reminders job: every 30 minutes")

	// Проверка здоровья WG Agent - каждые 5 минут
	_, err = s.cron.AddFunc("*/5 * * * *", s.healthCheckWGAgent)
	if err != nil {
		return errors.New("failed to add health check job: " + err.Error())
	}
	slog.Info("Added WG Agent health check job: every 5 minutes")

	// Запускаем планировщик
	s.cron.Start()
	slog.Info("Cron scheduler started successfully")

	return nil
}

func (s *Scheduler) Stop() {
	slog.Info("Stopping scheduler")
	s.cron.Stop()
	s.wgClient.Close()
	slog.Info("Scheduler stopped")
}

// Отключение просроченных подписок
func (s *Scheduler) disableExpiredSubscriptions() {
	slog.Info("Running expired subscriptions cleanup job")

	// Получаем просроченные подписки
	var expiredSubs []db.Subscription
	today := time.Now().Format("2006-01-02")

	result := s.repo.DB().Where("active = true AND end_date < ?", today).Find(&expiredSubs)
	if result.Error != nil {
		slog.Error("Failed to fetch expired subscriptions", "error", result.Error)
		s.sendCriticalAlert("❌ Ошибка получения просроченных подписок: " + result.Error.Error())
		return
	}

	if len(expiredSubs) == 0 {
		slog.Info("No expired subscriptions found")
		return
	}

	slog.Info("Found expired subscriptions", "count", len(expiredSubs))

	disabled := 0
	removed := 0
	ctx := context.Background()

	for _, sub := range expiredSubs {
		slog.Info("Processing expired subscription", "subscription_id", sub.ID, "peer_id", sub.PeerID, "end_date", sub.EndDate.Format("2006-01-02"))

		// Отключаем пира
		disableReq := &wgagent.DisablePeerRequest{
			Interface: sub.Interface,
			PublicKey: sub.PublicKey,
		}

		err := s.wgClient.DisablePeer(ctx, disableReq)
		if err != nil {
			slog.Error("Failed to disable expired peer", "peer_id", sub.PeerID, "error", err)
			continue
		}

		// Удаляем пира из интерфейса
		removeReq := &wgagent.RemovePeerRequest{
			Interface: sub.Interface,
			PublicKey: sub.PublicKey,
		}

		err = s.wgClient.RemovePeer(ctx, removeReq)
		if err != nil {
			slog.Error("Failed to remove expired peer", "peer_id", sub.PeerID, "error", err)
			disabled++
		} else {
			removed++
		}

		// Обновляем статус в БД
		if err := s.repo.DB().Model(&sub).Update("active", false).Error; err != nil {
			slog.Error("Failed to update subscription status", "subscription_id", sub.ID, "error", err)
		}
	}

	slog.Info("Expired subscriptions cleanup completed", "disabled", disabled, "removed", removed, "total_processed", len(expiredSubs))

	// Отправляем отчет админу
	s.sendAdminReport("🕒 Автоматическая очистка просроченных подписок:\n✅ Отключено: " + strconv.Itoa(disabled) + "\n🗑 Удалено: " + strconv.Itoa(removed) + "\n📊 Всего обработано: " + strconv.Itoa(len(expiredSubs)))
}

// Отправка напоминаний об истечении подписок
func (s *Scheduler) sendExpirationReminders() {
	slog.Debug("Checking for expiration reminders")

	// Подписки, истекающие через 3 дня
	threeDaysLater := time.Now().AddDate(0, 0, 3).Format("2006-01-02")

	var soonExpiringSubs []db.Subscription
	result := s.repo.DB().Where("active = true AND end_date = ?", threeDaysLater).
		Preload("User").
		Preload("Plan").
		Find(&soonExpiringSubs)

	if result.Error != nil {
		slog.Error("Failed to fetch soon expiring subscriptions", "error", result.Error)
		s.sendCriticalAlert("❌ Ошибка получения истекающих подписок: " + result.Error.Error())
		return
	}

	if len(soonExpiringSubs) == 0 {
		slog.Debug("No subscriptions expiring in 3 days")
		return
	}

	slog.Info("Found subscriptions expiring in 3 days", "count", len(soonExpiringSubs))

	sent := 0
	for _, sub := range soonExpiringSubs {
		slog.Info("Sending expiration reminder", "subscription_id", sub.ID, "user_id", sub.User.TgID, "plan", sub.Plan.Name)

		text := "⚠️ Напоминание о подписке\n\n" +
			"Ваша подписка \"" + sub.Plan.Name + "\" истекает через 3 дня (" + sub.EndDate.Format("02.01.2006") + ").\n\n" +
			"Не забудьте продлить подписку, чтобы не потерять доступ к VPN!\n\n" +
			"Для продления используйте команду /buy"

		msg := tgbotapi.NewMessage(sub.User.TgID, text)
		_, err := s.bot.Send(msg)
		if err != nil {
			slog.Error("Failed to send expiration reminder", "user_id", sub.User.TgID, "subscription_id", sub.ID, "error", err)
		} else {
			sent++
		}
	}

	if sent > 0 {
		slog.Info("Expiration reminders sent", "sent", sent, "total", len(soonExpiringSubs))
		s.sendAdminReport("📨 Отправлено напоминаний об истечении: " + strconv.Itoa(sent) + " из " + strconv.Itoa(len(soonExpiringSubs)))
	}
}

// Проверка здоровья WG Agent
func (s *Scheduler) healthCheckWGAgent() {
	slog.Debug("Performing WG Agent health check")

	// Создаем тестовый клиент для проверки
	wgConfig := wgagent.Config{
		Addr:     s.cfg.WGAgentAddr,
		CertFile: s.cfg.WGClientCert,
		KeyFile:  s.cfg.WGClientKey,
		CAFile:   s.cfg.WGCACert,
	}

	testClient, err := wgagent.NewClient(wgConfig)
	if err != nil {
		slog.Error("WG Agent health check failed", "error", err, "wg_addr", s.cfg.WGAgentAddr)
		s.sendHealthAlert("❌ WG-Agent недоступен: " + err.Error())
		return
	}
	defer testClient.Close()

	slog.Debug("WG Agent health check passed")
}

// Отправка отчета администратору
func (s *Scheduler) sendAdminReport(message string) {
	if s.cfg.SuperAdminID == "" {
		slog.Warn("Super admin ID not configured, cannot send report")
		return
	}

	adminID, err := strconv.ParseInt(s.cfg.SuperAdminID, 10, 64)
	if err != nil {
		slog.Error("Invalid super admin ID", "super_admin_id", s.cfg.SuperAdminID, "error", err)
		return
	}

	slog.Info("Sending admin report", "admin_id", adminID, "message_length", len(message))

	msg := tgbotapi.NewMessage(adminID, message)
	_, err = s.bot.Send(msg)
	if err != nil {
		slog.Error("Failed to send admin report", "admin_id", adminID, "error", err)
	}
}

// Отправка критического уведомления
func (s *Scheduler) sendCriticalAlert(message string) {
	slog.Error("Critical alert", "message", message)
	s.sendAdminReport("🚨 КРИТИЧЕСКАЯ ОШИБКА ПЛАНИРОВЩИКА\n\n" + message)
}

// Отправка уведомления о проблемах со здоровьем
func (s *Scheduler) sendHealthAlert(message string) {
	slog.Warn("Health alert", "message", message)
	s.sendAdminReport("🚨 " + message)
}
