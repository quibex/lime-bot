package telegram

import (
	"fmt"
	"log/slog"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Error коды для различных типов ошибок
const (
	ErrInvalidInput      = "INVALID_INPUT"
	ErrDatabaseError     = "DATABASE_ERROR"
	ErrWGAgentError      = "WGAGENT_ERROR"
	ErrPermissionDenied  = "PERMISSION_DENIED"
	ErrUserNotFound      = "USER_NOT_FOUND"
	ErrPlanNotFound      = "PLAN_NOT_FOUND"
	ErrPaymentError      = "PAYMENT_ERROR"
	ErrSubscriptionError = "SUBSCRIPTION_ERROR"
)

// BotError представляет ошибку бота с кодом и сообщением для пользователя
type BotError struct {
	Code        string
	Message     string
	UserMessage string
	Details     string
}

func (e *BotError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Code, e.Message, e.Details)
}

// NewBotError создает новую ошибку бота
func NewBotError(code, message, userMessage, details string) *BotError {
	return &BotError{
		Code:        code,
		Message:     message,
		UserMessage: userMessage,
		Details:     details,
	}
}

// handleError обрабатывает ошибки и отправляет соответствующие сообщения пользователю
func (s *Service) handleError(chatID int64, err error) {
	slog.Error("Bot error occurred", "error", err)

	var userMessage string

	// Проверяем, является ли ошибка нашей BotError
	if botErr, ok := err.(*BotError); ok {
		userMessage = botErr.UserMessage

		// Отправляем детали ошибки супер-админу
		s.sendErrorReport(botErr)
	} else {
		// Общая ошибка
		userMessage = "Произошла внутренняя ошибка. Попробуйте позже."

		// Отправляем отчет об ошибке
		s.sendErrorReport(&BotError{
			Code:        "UNKNOWN_ERROR",
			Message:     "Unknown error occurred",
			UserMessage: userMessage,
			Details:     err.Error(),
		})
	}

	// Отправляем сообщение пользователю
	s.reply(chatID, "❌ "+userMessage)
}

// sendErrorReport отправляет отчет об ошибке супер-админу
func (s *Service) sendErrorReport(botErr *BotError) {
	if s.cfg.SuperAdminID == "" {
		return
	}

	adminID, err := strconv.ParseInt(s.cfg.SuperAdminID, 10, 64)
	if err != nil {
		return
	}

	report := fmt.Sprintf(`🚨 Ошибка в боте:

Код: %s
Сообщение: %s
Детали: %s

Пользователю показано: %s`,
		botErr.Code,
		botErr.Message,
		botErr.Details,
		botErr.UserMessage,
	)

	msg := tgbotapi.NewMessage(adminID, report)
	s.bot.Send(msg)
}

// Вспомогательные функции для создания типичных ошибок

func ErrInvalidInputf(details string, args ...interface{}) *BotError {
	return NewBotError(
		ErrInvalidInput,
		"Invalid input provided",
		"Неверный формат данных. Проверьте правильность ввода.",
		fmt.Sprintf(details, args...),
	)
}

func ErrDatabasef(details string, args ...interface{}) *BotError {
	return NewBotError(
		ErrDatabaseError,
		"Database operation failed",
		"Ошибка базы данных. Попробуйте позже.",
		fmt.Sprintf(details, args...),
	)
}

func ErrWGAgentf(details string, args ...interface{}) *BotError {
	return NewBotError(
		ErrWGAgentError,
		"WG-Agent operation failed",
		"Ошибка настройки VPN. Обратитесь к администратору.",
		fmt.Sprintf(details, args...),
	)
}

func ErrPermission(details string) *BotError {
	return NewBotError(
		ErrPermissionDenied,
		"Permission denied",
		"У вас нет прав для выполнения этой операции.",
		details,
	)
}

func ErrUserNotFoundf(details string, args ...interface{}) *BotError {
	return NewBotError(
		ErrUserNotFound,
		"User not found",
		"Пользователь не найден.",
		fmt.Sprintf(details, args...),
	)
}

func ErrPlanNotFoundf(details string, args ...interface{}) *BotError {
	return NewBotError(
		ErrPlanNotFound,
		"Plan not found",
		"Тариф не найден или недоступен.",
		fmt.Sprintf(details, args...),
	)
}

func ErrPaymentf(details string, args ...interface{}) *BotError {
	return NewBotError(
		ErrPaymentError,
		"Payment processing failed",
		"Ошибка обработки платежа. Обратитесь к администратору.",
		fmt.Sprintf(details, args...),
	)
}

func ErrSubscriptionf(details string, args ...interface{}) *BotError {
	return NewBotError(
		ErrSubscriptionError,
		"Subscription operation failed",
		"Ошибка управления подпиской. Обратитесь к администратору.",
		fmt.Sprintf(details, args...),
	)
}
