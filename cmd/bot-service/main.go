package main

import (
	"context"
	"log/slog"
	"os/signal"
	"syscall"

	"lime-bot/internal/config"
	"lime-bot/internal/db"
	"lime-bot/internal/scheduler"
	"lime-bot/internal/telegram"
)

func main() {
	slog.Info("bot-service стартует...")

	// Инициализация конфига
	cfg := config.Load()

	// Подключение к БД
	repo, err := db.NewRepository(cfg.DBDsn)
	if err != nil {
		slog.Error("Ошибка подключения к БД", "error", err)
		return
	}

	// Автомиграция
	if err := repo.AutoMigrate(); err != nil {
		slog.Error("Ошибка миграции БД", "error", err)
		return
	}

	// Создание Telegram-бота
	telegramService, err := telegram.New(cfg, repo)
	if err != nil {
		slog.Error("Ошибка создания бота", "error", err)
		return
	}

	// Создание планировщика
	scheduler, err := scheduler.NewScheduler(repo, telegramService.Bot(), cfg)
	if err != nil {
		slog.Error("Ошибка создания планировщика", "error", err)
		return
	}

	// Контекст для graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Запуск планировщика
	if err := scheduler.Start(); err != nil {
		slog.Error("Ошибка запуска планировщика", "error", err)
		return
	}
	defer scheduler.Stop()

	// Запуск бота
	slog.Info("Запуск Telegram-бота...")
	if err := telegramService.Start(ctx); err != nil {
		slog.Warn("Бот остановлен", "error", err)
	}
}
