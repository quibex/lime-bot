package main

import (
	"context"
	"log/slog"
	"os/signal"
	"syscall"

	"lime-bot/internal/config"
	"lime-bot/internal/db"
	"lime-bot/internal/health"
	"lime-bot/internal/scheduler"
	"lime-bot/internal/telegram"
)

func main() {
	slog.Info("bot-service стартует...")

	cfg := config.Load()

	repo, err := db.NewRepository(cfg.DBDsn)
	if err != nil {
		slog.Error("Ошибка подключения к БД", "error", err)
		return
	}

	if err := repo.AutoMigrate(); err != nil {
		slog.Error("Ошибка миграции БД", "error", err)
		return
	}

	telegramService, err := telegram.New(cfg, repo)
	if err != nil {
		slog.Error("Ошибка создания бота", "error", err)
		return
	}

	scheduler, err := scheduler.NewScheduler(repo, telegramService.Bot(), cfg)
	if err != nil {
		slog.Error("Ошибка создания планировщика", "error", err)
		return
	}

	healthServer := health.NewServer(cfg.HealthAddr)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		if err := healthServer.Start(); err != nil {
			slog.Error("Health сервер завершен", "error", err)
		}
	}()
	defer healthServer.Stop()

	if err := scheduler.Start(); err != nil {
		slog.Error("Ошибка запуска планировщика", "error", err)
		return
	}
	defer scheduler.Stop()

	slog.Info("Запуск Telegram-бота...")
	if err := telegramService.Start(ctx); err != nil {
		slog.Warn("Бот остановлен", "error", err)
	}
}
