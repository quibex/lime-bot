# Рекомендации по рефакторингу lime-bot

*Переход к простой чистой архитектуре с 3 слоями (обновлено)*

## Обзор целевой архитектуры

После анализа текущих улучшений предлагается переход к простой чистой архитектуре с 3 слоями:

1. **Transport Layer** (Транспортный слой) - Telegram API
2. **Service Layer** (Сервисный слой) - бизнес-логика в stories  
3. **Storage Layer** (Слой данных) - репозитории и БД

**Принцип:** каждая история (story) = один use case с интерфейсами зависимостей

## Целевая структура проекта

```
lime-bot/
├── cmd/bot-service/
│   └── main.go                     # Простая инициализация
├── internal/
│   ├── config/                     # Конфигурация (как есть)
│   ├── environment/                # Env со всеми зависимостями  
│   ├── transport/                  # TRANSPORT LAYER
│   │   └── telegram/              # Telegram бот (адаптер)
│   ├── stories/                    # SERVICE LAYER  
│   │   ├── createsubscription/    # Создание подписки
│   │   ├── getmykeys/             # Получение ключей
│   │   ├── processpayment/        # Обработка платежа
│   │   ├── disablesubscription/   # Отключение подписки
│   │   └── manageadmins/          # Управление админами
│   ├── storage/                    # STORAGE LAYER
│   │   ├── postgres/              # PostgreSQL репозитории
│   │   └── redis/                 # Redis для состояний
│   ├── clients/                    # Внешние клиенты
│   │   └── wgagent/               # WireGuard agent
│   └── models/                     # Модели данных
```

## Service Layer - Stories

### Структура story

Каждая история содержит:

- `contracts.go` - интерфейсы зависимостей
- `story.go` - основная логика

### Пример: stories/createsubscription

**internal/stories/createsubscription/contracts.go**

```go
package createsubscription

import (
    "context"
    "lime-bot/internal/models"
)

type Manager interface {
    CreateSubscription(ctx context.Context, req CreateSubscriptionRequest) (*CreateSubscriptionResponse, error)
}

type Storage interface {
    GetUser(ctx context.Context, telegramID int64) (*models.User, error)
    GetPlan(ctx context.Context, planID int64) (*models.Plan, error)
    CreatePayment(ctx context.Context, payment *models.Payment) error
    CreateSubscription(ctx context.Context, subscription *models.Subscription) error
    CountActiveSubscriptions(ctx context.Context, userID int64) (int, error)
}

type WGClient interface {
    GenerateConfig(ctx context.Context, req GenerateConfigRequest) (*GenerateConfigResponse, error)
}

type Logger interface {
    Info(msg string, args ...interface{})
    Error(msg string, args ...interface{})
}

type CreateSubscriptionRequest struct {
    TelegramID      int64
    PlanID          int64
    Platform        string
    Quantity        int
    PaymentMethodID int64
}

type CreateSubscriptionResponse struct {
    PaymentID     int64
    Subscriptions []*models.Subscription
    Instructions  string
}
```

**internal/stories/createsubscription/story.go**

```go
package createsubscription

import (
    "context"
    "fmt"
    "time"
    
    "lime-bot/internal/models"
)

type story struct {
    storage  Storage
    wgClient WGClient
    logger   Logger
}

func NewManager(storage Storage, wgClient WGClient, logger Logger) Manager {
    return &story{
        storage:  storage,
        wgClient: wgClient,
        logger:   logger,
    }
}

func (s *story) CreateSubscription(ctx context.Context, req CreateSubscriptionRequest) (*CreateSubscriptionResponse, error) {
    s.logger.Info("Creating subscription", "telegram_id", req.TelegramID, "plan_id", req.PlanID)
    
    // 1. Получаем пользователя
    user, err := s.storage.GetUser(ctx, req.TelegramID)
    if err != nil {
        return nil, fmt.Errorf("пользователь не найден")
    }
    
    // 2. Получаем план
    plan, err := s.storage.GetPlan(ctx, req.PlanID)
    if err != nil {
        return nil, fmt.Errorf("план не найден")
    }
    
    // 3. Проверяем лимиты
    activeCount, err := s.storage.CountActiveSubscriptions(ctx, user.ID)
    if err != nil {
        return nil, fmt.Errorf("ошибка проверки лимитов")
    }
    
    if activeCount >= 5 {
        return nil, fmt.Errorf("слишком много активных подписок")
    }
    
    // 4. Создаем платеж
    payment := &models.Payment{
        UserID:          user.ID,
        PlanID:          req.PlanID,
        Amount:          plan.Price * float64(req.Quantity),
        PaymentMethodID: req.PaymentMethodID,
        Status:          "pending",
        CreatedAt:       time.Now(),
    }
    
    if err := s.storage.CreatePayment(ctx, payment); err != nil {
        return nil, fmt.Errorf("ошибка создания платежа")
    }
    
    // 5. Создаем подписки
    var subscriptions []*models.Subscription
    
    for i := 0; i < req.Quantity; i++ {
        subscription, err := s.createSingleSubscription(ctx, user, plan, req.Platform, payment.ID)
        if err != nil {
            continue // частичный успех
        }
        subscriptions = append(subscriptions, subscription)
    }
    
    if len(subscriptions) == 0 {
        return nil, fmt.Errorf("не удалось создать ни одной подписки")
    }
    
    return &CreateSubscriptionResponse{
        PaymentID:     payment.ID,
        Subscriptions: subscriptions,
        Instructions:  fmt.Sprintf("💰 К оплате: %.2f руб.", payment.Amount),
    }, nil
}

func (s *story) createSingleSubscription(ctx context.Context, user *models.User, plan *models.Plan, platform string, paymentID int64) (*models.Subscription, error) {
    // Генерируем WG конфиг
    peerID := fmt.Sprintf("user%d_%d", user.ID, time.Now().Unix())
    
    wgResp, err := s.wgClient.GenerateConfig(ctx, GenerateConfigRequest{
        Platform: platform,
        PeerID:   peerID,
    })
    if err != nil {
        return nil, err
    }
    
    subscription := &models.Subscription{
        UserID:     user.ID,
        PlanID:     plan.ID,
        PaymentID:  paymentID,
        PeerID:     peerID,
        Platform:   platform,
        Config:     wgResp.Config,
        StartDate:  time.Now(),
        EndDate:    time.Now().AddDate(0, 0, plan.DurationDays),
        Active:     false,
        CreatedAt:  time.Now(),
    }
    
    return subscription, s.storage.CreateSubscription(ctx, subscription)
}
```

### Пример: stories/getmykeys

**internal/stories/getmykeys/contracts.go**

```go
package getmykeys

import (
    "context"
    "lime-bot/internal/models"
)

type Manager interface {
    GetMyKeys(ctx context.Context, req GetMyKeysRequest) (*GetMyKeysResponse, error)
}

type Storage interface {
    GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error)
    GetActiveSubscriptions(ctx context.Context, userID int64) ([]*models.Subscription, error)
}

type GetMyKeysRequest struct {
    TelegramID int64
}

type GetMyKeysResponse struct {
    Subscriptions []*models.Subscription
}
```

## Environment - управление зависимостями

**internal/environment/env.go**

```go
package environment

import (
    "context"
    "log/slog"
    
    "lime-bot/internal/config"
    "lime-bot/internal/clients/wgagent"
    "lime-bot/internal/storage/postgres"
    "lime-bot/internal/stories/createsubscription"
    "lime-bot/internal/stories/getmykeys"
)

type Env struct {
    Config   *config.Config
    Logger   *slog.Logger
    
    // Storage
    Postgres *postgres.Storage
    
    // Clients  
    WGAgent *wgagent.Client
    
    // Stories
    CreateSubscriptionMgr createsubscription.Manager
    GetMyKeysMgr         getmykeys.Manager
}

func Setup(ctx context.Context) (*Env, error) {
    cfg := config.Load()
    
    logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    }))
    
    // Storage
    pgStorage, err := postgres.NewStorage(cfg.DBDsn)
    if err != nil {
        return nil, err
    }
    
    // Clients
    wgClient, err := wgagent.NewClient(cfg.WGAgentAddr)
    if err != nil {
        return nil, err
    }
    
    env := &Env{
        Config:   cfg,
        Logger:   logger,
        Postgres: pgStorage,
        WGAgent:  wgClient,
    }
    
    // Stories
    env.CreateSubscriptionMgr = createsubscription.NewManager(
        env.Postgres,
        env.WGAgent,
        env.Logger,
    )
    
    env.GetMyKeysMgr = getmykeys.NewManager(
        env.Postgres,
        env.Logger,
    )
    
    return env, nil
}

func (e *Env) Close() error {
    if e.Postgres != nil {
        e.Postgres.Close()
    }
    if e.WGAgent != nil {
        e.WGAgent.Close()
    }
    return nil
}
```

## Transport Layer - Telegram

**internal/transport/telegram/bot.go**

```go
package telegram

import (
    "context"
    "fmt"
    
    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
    
    "lime-bot/internal/environment"
    "lime-bot/internal/stories/createsubscription"
    "lime-bot/internal/stories/getmykeys"
)

type Bot struct {
    api *tgbotapi.BotAPI
    env *environment.Env
}

func NewBot(token string, env *environment.Env) (*Bot, error) {
    api, err := tgbotapi.NewBotAPI(token)
    if err != nil {
        return nil, err
    }
    
    return &Bot{api: api, env: env}, nil
}

func (b *Bot) Start(ctx context.Context) error {
    u := tgbotapi.NewUpdate(0)
    u.Timeout = 60
    
    updates := b.api.GetUpdatesChan(u)
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case update := <-updates:
            go b.handleUpdate(ctx, update)
        }
    }
}

func (b *Bot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
    if update.Message != nil && update.Message.IsCommand() {
        b.handleCommand(ctx, update.Message)
    }
}

func (b *Bot) handleCommand(ctx context.Context, msg *tgbotapi.Message) {
    switch msg.Command() {
    case "mykeys":
        b.handleMyKeysCommand(ctx, msg)
    default:
        b.sendMessage(msg.Chat.ID, "Неизвестная команда")
    }
}

func (b *Bot) handleMyKeysCommand(ctx context.Context, msg *tgbotapi.Message) {
    req := getmykeys.GetMyKeysRequest{
        TelegramID: msg.From.ID,
    }
    
    resp, err := b.env.GetMyKeysMgr.GetMyKeys(ctx, req)
    if err != nil {
        b.sendMessage(msg.Chat.ID, "❌ "+err.Error())
        return
    }
    
    if len(resp.Subscriptions) == 0 {
        b.sendMessage(msg.Chat.ID, "У вас нет активных подписок")
        return
    }
    
    text := "🔑 Ваши подписки:\n\n"
    for i, sub := range resp.Subscriptions {
        text += fmt.Sprintf("%d. %s (%s)\n", i+1, sub.Platform, sub.PeerID)
    }
    
    b.sendMessage(msg.Chat.ID, text)
}

func (b *Bot) sendMessage(chatID int64, text string) {
    msg := tgbotapi.NewMessage(chatID, text)
    b.api.Send(msg)
}
```

## Storage Layer

**internal/storage/postgres/storage.go**

```go
package postgres

import (
    "context"
    "database/sql"
    "lime-bot/internal/models"
    
    _ "github.com/lib/pq"
)

type Storage struct {
    db *sql.DB
}

func NewStorage(dsn string) (*Storage, error) {
    db, err := sql.Open("postgres", dsn)
    if err != nil {
        return nil, err
    }
    
    return &Storage{db: db}, nil
}

func (s *Storage) GetUser(ctx context.Context, telegramID int64) (*models.User, error) {
    query := "SELECT id, telegram_id, username, created_at FROM users WHERE telegram_id = $1"
    
    var user models.User
    err := s.db.QueryRowContext(ctx, query, telegramID).Scan(
        &user.ID, &user.TelegramID, &user.Username, &user.CreatedAt,
    )
    
    return &user, err
}

func (s *Storage) GetUserByTelegramID(ctx context.Context, telegramID int64) (*models.User, error) {
    return s.GetUser(ctx, telegramID)
}

func (s *Storage) GetActiveSubscriptions(ctx context.Context, userID int64) ([]*models.Subscription, error) {
    query := `
        SELECT id, user_id, plan_id, peer_id, platform, config, 
               start_date, end_date, active, created_at
        FROM subscriptions 
        WHERE user_id = $1 AND active = true
    `
    
    rows, err := s.db.QueryContext(ctx, query, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    var subscriptions []*models.Subscription
    for rows.Next() {
        var sub models.Subscription
        err := rows.Scan(
            &sub.ID, &sub.UserID, &sub.PlanID, &sub.PeerID,
            &sub.Platform, &sub.Config, &sub.StartDate,
            &sub.EndDate, &sub.Active, &sub.CreatedAt,
        )
        if err != nil {
            return nil, err
        }
        subscriptions = append(subscriptions, &sub)
    }
    
    return subscriptions, nil
}

func (s *Storage) Close() error {
    return s.db.Close()
}
```

## Models

**internal/models/models.go**

```go
package models

import "time"

type User struct {
    ID         int64     `json:"id"`
    TelegramID int64     `json:"telegram_id"`
    Username   string    `json:"username"`
    CreatedAt  time.Time `json:"created_at"`
}

type Plan struct {
    ID           int64   `json:"id"`
    Name         string  `json:"name"`
    Price        float64 `json:"price"`
    DurationDays int     `json:"duration_days"`
}

type Payment struct {
    ID              int64     `json:"id"`
    UserID          int64     `json:"user_id"`
    PlanID          int64     `json:"plan_id"`
    Amount          float64   `json:"amount"`
    PaymentMethodID int64     `json:"payment_method_id"`
    Status          string    `json:"status"`
    CreatedAt       time.Time `json:"created_at"`
}

type Subscription struct {
    ID        int64     `json:"id"`
    UserID    int64     `json:"user_id"`
    PlanID    int64     `json:"plan_id"`
    PaymentID int64     `json:"payment_id"`
    PeerID    string    `json:"peer_id"`
    Platform  string    `json:"platform"`
    Config    string    `json:"config"`
    StartDate time.Time `json:"start_date"`
    EndDate   time.Time `json:"end_date"`
    Active    bool      `json:"active"`
    CreatedAt time.Time `json:"created_at"`
}
```

## Упрощенный main.go

**cmd/bot-service/main.go**

```go
package main

import (
    "context"
    "log"
    "os/signal"
    "syscall"
    
    "lime-bot/internal/environment"
    "lime-bot/internal/transport/telegram"
)

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()
    
    // Инициализируем окружение
    env, err := environment.Setup(ctx)
    if err != nil {
        log.Fatalf("Failed to setup environment: %v", err)
    }
    defer env.Close()
    
    env.Logger.Info("Environment initialized")
    
    // Создаем бота
    bot, err := telegram.NewBot(env.Config.BotToken, env)
    if err != nil {
        log.Fatalf("Failed to create bot: %v", err)
    }
    
    env.Logger.Info("Starting bot...")
    
    // Запускаем
    if err := bot.Start(ctx); err != nil {
        env.Logger.Error("Bot error", "error", err)
    }
    
    env.Logger.Info("Bot stopped")
}
```

## Что сохранить из улучшений

✅ **Систему ошибок** - использовать в transport layer для отчетов админу
✅ **Логирование** - через environment во всех stories
✅ **Scheduler** - обернуть в stories/scheduledjobs
✅ **Graceful shutdown** - в main.go

## План миграции

### Этап 1: Создание структуры

1. Создать `internal/environment/`
2. Создать `internal/models/`
3. Создать `internal/storage/postgres/`
4. Перенести telegram в `internal/transport/telegram/`

### Этап 2: Первые stories

1. `stories/getmykeys` - простая для начала
2. `stories/createsubscription` - основная логика
3. Интеграция с существующими ошибками

### Этап 3: Остальные stories

1. `stories/processpayment` - админская логика
2. `stories/manageadmins` - управление админами
3. `stories/scheduledjobs` - обертка scheduler

### Этап 4: Очистка

1. Удалить старые файлы `buy.go`, `admin.go`
2. Упростить telegram handlers
3. Добавить тесты для stories

## Преимущества

✅ **Простота** - минимум абстракций
✅ **Понятность** - каждая story = один use case  
✅ **Тестируемость** - легко мокать интерфейсы
✅ **Практичность** - архитектура под размер проекта
