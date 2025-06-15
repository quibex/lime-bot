package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"lime-bot/internal/config"
	"lime-bot/internal/db"
	"lime-bot/internal/gates/wgagent"
	"lime-bot/internal/health"
	"lime-bot/internal/scheduler"
	"lime-bot/internal/telegram"
	"lime-bot/internal/wgtest"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	// Настраиваем структурированное логирование
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	}))
	slog.SetDefault(logger)

	slog.Info("Starting bot-service", "version", "1.0.0", "pid", os.Getpid())

	// Загружаем конфигурацию
	cfg := config.Load()
	slog.Info("Configuration loaded",
		"db_dsn", cfg.DBDsn,
		"wg_agent_addr", cfg.WGAgentAddr,
		"health_addr", cfg.HealthAddr,
		"has_super_admin", cfg.SuperAdminID != "",
		"has_bot_token", cfg.BotToken != "",
	)

	if cfg.BotToken == "" {
		slog.Error("Bot token is not configured")
		os.Exit(1)
	}

	// Инициализируем репозиторий
	repo, err := db.NewRepository(cfg.DBDsn)
	if err != nil {
		slog.Error("Failed to initialize database repository", "error", err, "dsn", cfg.DBDsn)
		os.Exit(1)
	}
	slog.Info("Database repository initialized successfully")

	// Выполняем миграции
	if err := repo.AutoMigrate(); err != nil {
		slog.Error("Database migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("Database migrations completed successfully")

	// Создаем Telegram сервис
	telegramService, err := telegram.New(cfg, repo)
	if err != nil {
		slog.Error("Failed to create Telegram service", "error", err)
		os.Exit(1)
	}
	slog.Info("Telegram service created successfully")

	// Настраиваем WG Agent конфиг
	wgConfig := wgagent.Config{
		Addr:     cfg.WGAgentAddr,
		CertFile: cfg.WGClientCert,
		KeyFile:  cfg.WGClientKey,
		CAFile:   cfg.WGCACert,
	}

	// Если сертификаты не настроены, используем insecure соединение
	if cfg.WGClientCert == "" || cfg.WGClientKey == "" || cfg.WGCACert == "" {
		slog.Warn("WG certificates not configured, using insecure connection")
		wgConfig = wgagent.Config{
			Addr: cfg.WGAgentAddr,
		}
	}

	// Создаем функцию уведомления суперадмина
	notifyFn := func(message string) {
		if cfg.SuperAdminID == "" {
			slog.Warn("SuperAdminID not configured, cannot send notification")
			return
		}

		superAdminID, parseErr := strconv.ParseInt(cfg.SuperAdminID, 10, 64)
		if parseErr != nil {
			slog.Error("Invalid SuperAdminID format", "super_admin_id", cfg.SuperAdminID, "error", parseErr)
			return
		}

		msg := tgbotapi.NewMessage(superAdminID, message)
		_, sendErr := telegramService.Bot().Send(msg)
		if sendErr != nil {
			slog.Error("Failed to send notification to super admin", "error", sendErr, "super_admin_id", superAdminID)
		} else {
			slog.Info("Notification sent to super admin", "super_admin_id", superAdminID)
		}
	}

	// Создаем интеграционный тест
	wgIntegrationTest := wgtest.NewIntegrationTest(wgConfig, notifyFn)

	// Запускаем стартовый тест в горутине (не блокируем запуск)
	go func() {
		testCtx, testCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer testCancel()

		if err := wgIntegrationTest.RunStartupTest(testCtx); err != nil {
			slog.Error("WG Agent startup test failed", "error", err)
			// Не останавливаем приложение, продолжаем работу
		}
	}()

	// Создаем планировщик
	scheduler, err := scheduler.NewScheduler(repo, telegramService.Bot(), cfg)
	if err != nil {
		slog.Error("Failed to create scheduler", "error", err)
		os.Exit(1)
	}
	slog.Info("Scheduler created successfully")

	// Создаем health сервер
	healthServer := health.NewServer(cfg.HealthAddr)
	slog.Info("Health server created", "addr", cfg.HealthAddr)

	// Настраиваем graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Запускаем health сервер в горутине
	go func() {
		slog.Info("Starting health server")
		if err := healthServer.Start(); err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Error("Health server failed", "error", err)
			} else {
				slog.Info("Health server stopped")
			}
		}
	}()
	defer func() {
		slog.Info("Stopping health server")
		if err := healthServer.Stop(); err != nil {
			slog.Error("Failed to stop health server", "error", err)
		}
	}()

	// Запускаем планировщик если он создан
	if scheduler != nil {
		if err := scheduler.Start(); err != nil {
			slog.Error("Failed to start scheduler", "error", err)
			slog.Warn("Continuing without scheduler")
		} else {
			slog.Info("Scheduler started successfully")
			defer func() {
				slog.Info("Stopping scheduler")
				scheduler.Stop()
			}()
		}
	}

	// Запускаем периодический health check WG Agent
	go wgIntegrationTest.RunPeriodicHealthCheck(ctx, 5*time.Minute)

	// Запускаем Telegram бота
	slog.Info("Starting Telegram bot...")
	if err := telegramService.Start(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			slog.Info("Telegram bot stopped by signal")
		} else {
			slog.Error("Telegram bot failed", "error", err)
			os.Exit(1)
		}
	}

	slog.Info("Bot service shutdown completed")
}
