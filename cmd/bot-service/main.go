package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"lime-bot/internal/config"
	"lime-bot/internal/db"
	"lime-bot/internal/health"
	"lime-bot/internal/scheduler"
	"lime-bot/internal/telegram"
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

	// Создаем планировщик
	scheduler, err := scheduler.NewScheduler(repo, telegramService.Bot(), cfg)
	if err != nil {
		slog.Error("Failed to create scheduler", "error", err)

		// Пытаемся продолжить без планировщика
		slog.Warn("Continuing without scheduler - some background tasks will not work")
		scheduler = nil
	} else {
		slog.Info("Scheduler created successfully")
	}

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
