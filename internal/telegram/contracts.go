package telegram

// Plan описывает тариф.
type Plan struct {
	ID           uint
	Name         string
	Price        int // цена в минимальных единицах валюты (integer)
	DurationDays int
}

// Repository описывает методы работы с БД, необходимые Telegram-сервису.
type Repository interface {
	// ListPlans возвращает все доступные планы подписки.
	ListPlans() ([]Plan, error)

	// RegisterUser создаёт запись о пользователе, если её нет.
	RegisterUser(tgID int64, username string) error
}

// WGAgentClient описывает взаимодействие с внешним gRPC-сервисом wg-agent.
type WGAgentClient interface {
	// GenerateConfig создаёт конфигурацию WireGuard для пользователя и возвращает её текст.
	GenerateConfig(userID int64) (string, error)

	// RevokeConfig отзывает конфигурацию.
	RevokeConfig(userID int64) error
}

// Payments описывает платежный провайдер.
type Payments interface {
	// CreateInvoice генерирует ссылку/QR на оплату выбранного плана.
	CreateInvoice(userID int64, planID uint) (paymentURL string, err error)

	// VerifyPayment проверяет факт оплаты.
	VerifyPayment(invoiceID string) (paid bool, err error)
}
