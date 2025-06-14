package telegram

import (
	"errors"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	ErrInvalidInput      = "INVALID_INPUT"
	ErrDatabaseError     = "DATABASE_ERROR"
	ErrWGAgentError      = "WGAGENT_ERROR"
	ErrPermissionDenied  = "PERMISSION_DENIED"
	ErrUserNotFound      = "USER_NOT_FOUND"
	ErrPlanNotFound      = "PLAN_NOT_FOUND"
	ErrPaymentError      = "PAYMENT_ERROR"
	ErrSubscriptionError = "SUBSCRIPTION_ERROR"
	ErrConfigError       = "CONFIG_ERROR"
	ErrNetworkError      = "NETWORK_ERROR"
	ErrValidationError   = "VALIDATION_ERROR"
)

type BotError struct {
	Code        string
	Message     string
	UserMessage string
	Details     string
	Context     map[string]interface{}
	Timestamp   time.Time
	StackTrace  string
}

func (e *BotError) Error() string {
	return e.Code + ": " + e.Message + " | " + e.Details
}

func NewBotError(code, message, userMessage, details string) *BotError {
	return &BotError{
		Code:        code,
		Message:     message,
		UserMessage: userMessage,
		Details:     details,
		Context:     make(map[string]interface{}),
		Timestamp:   time.Now(),
		StackTrace:  getStackTrace(),
	}
}

func (e *BotError) WithContext(key string, value interface{}) *BotError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

func getStackTrace() string {
	const depth = 32
	var pcs [depth]uintptr
	n := runtime.Callers(3, pcs[:])

	var sb strings.Builder
	frames := runtime.CallersFrames(pcs[:n])

	for {
		frame, more := frames.Next()
		sb.WriteString(frame.Function)
		sb.WriteString(" (")
		sb.WriteString(frame.File)
		sb.WriteString(":")
		sb.WriteString(strconv.Itoa(frame.Line))
		sb.WriteString(")\n")

		if !more {
			break
		}
	}

	return sb.String()
}

func (s *Service) handleError(chatID int64, err error) {
	slog.Error("Bot error occurred",
		"error", err,
		"chat_id", chatID,
		"timestamp", time.Now(),
	)

	var userMessage string
	var botErr *BotError

	if errors.As(err, &botErr) {
		userMessage = botErr.UserMessage
		s.sendDetailedErrorReport(botErr.WithContext("chat_id", chatID))
	} else {
		userMessage = "Произошла внутренняя ошибка. Попробуйте позже."

		botErr = &BotError{
			Code:        "UNKNOWN_ERROR",
			Message:     "Unknown error occurred",
			UserMessage: userMessage,
			Details:     err.Error(),
			Context:     map[string]interface{}{"chat_id": chatID},
			Timestamp:   time.Now(),
			StackTrace:  getStackTrace(),
		}

		s.sendDetailedErrorReport(botErr)
	}

	s.reply(chatID, "❌ "+userMessage)
}

func (s *Service) logAndReportError(operation string, err error, context map[string]interface{}) {
	slog.Error("Operation failed",
		"operation", operation,
		"error", err,
		"context", context,
		"timestamp", time.Now(),
	)

	var botErr *BotError
	if !errors.As(err, &botErr) {
		botErr = &BotError{
			Code:        "OPERATION_ERROR",
			Message:     operation + " failed",
			UserMessage: "Операция завершилась с ошибкой",
			Details:     err.Error(),
			Context:     context,
			Timestamp:   time.Now(),
			StackTrace:  getStackTrace(),
		}
	}

	if botErr.Context == nil {
		botErr.Context = make(map[string]interface{})
	}

	for k, v := range context {
		botErr.Context[k] = v
	}

	s.sendDetailedErrorReport(botErr)
}

func (s *Service) sendDetailedErrorReport(botErr *BotError) {
	if s.cfg.SuperAdminID == "" {
		return
	}

	adminID, err := strconv.ParseInt(s.cfg.SuperAdminID, 10, 64)
	if err != nil {
		slog.Error("Invalid super admin ID", "super_admin_id", s.cfg.SuperAdminID)
		return
	}

	var contextStr strings.Builder
	if len(botErr.Context) > 0 {
		contextStr.WriteString("\n🔍 Контекст:\n")
		for k, v := range botErr.Context {
			contextStr.WriteString("• ")
			contextStr.WriteString(k)
			contextStr.WriteString(": ")
			contextStr.WriteString(stringify(v))
			contextStr.WriteString("\n")
		}
	}

	report := "🚨 КРИТИЧЕСКАЯ ОШИБКА В БОТЕ\n\n" +
		"⏰ Время: " + botErr.Timestamp.Format("02.01.2006 15:04:05") + "\n" +
		"🏷 Код: " + botErr.Code + "\n" +
		"📝 Сообщение: " + botErr.Message + "\n" +
		"📋 Детали: " + botErr.Details + "\n" +
		contextStr.String() +
		"\n👤 Пользователю показано: " + botErr.UserMessage

	// Отправляем основной отчет
	msg := tgbotapi.NewMessage(adminID, report)
	msg.ParseMode = "HTML"
	s.bot.Send(msg)

	// Отправляем stack trace отдельным сообщением если он не пустой
	if botErr.StackTrace != "" {
		stackMsg := "🔍 Stack Trace:\n```\n" + botErr.StackTrace + "\n```"
		stackMessage := tgbotapi.NewMessage(adminID, stackMsg)
		stackMessage.ParseMode = "Markdown"
		s.bot.Send(stackMessage)
	}
}

func stringify(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case int, int8, int16, int32, int64:
		return strconv.FormatInt(int64(val.(int64)), 10)
	case uint, uint8, uint16, uint32, uint64:
		return strconv.FormatUint(uint64(val.(uint64)), 10)
	case float32, float64:
		return strconv.FormatFloat(float64(val.(float64)), 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	default:
		return "unknown"
	}
}

// Функции-помощники для создания ошибок
func ErrInvalidInputf(details string, args ...interface{}) *BotError {
	detailsStr := details
	if len(args) > 0 {
		// Простая замена %v без fmt
		for _, arg := range args {
			detailsStr = strings.Replace(detailsStr, "%v", stringify(arg), 1)
		}
	}

	return NewBotError(
		ErrInvalidInput,
		"Invalid input provided",
		"Неверный формат данных. Проверьте правильность ввода.",
		detailsStr,
	)
}

func ErrDatabasef(details string, args ...interface{}) *BotError {
	detailsStr := details
	if len(args) > 0 {
		for _, arg := range args {
			detailsStr = strings.Replace(detailsStr, "%v", stringify(arg), 1)
		}
	}

	return NewBotError(
		ErrDatabaseError,
		"Database operation failed",
		"Ошибка базы данных. Попробуйте позже.",
		detailsStr,
	)
}

func ErrWGAgentf(details string, args ...interface{}) *BotError {
	detailsStr := details
	if len(args) > 0 {
		for _, arg := range args {
			detailsStr = strings.Replace(detailsStr, "%v", stringify(arg), 1)
		}
	}

	return NewBotError(
		ErrWGAgentError,
		"WG-Agent operation failed",
		"Ошибка настройки VPN. Обратитесь к администратору.",
		detailsStr,
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
	detailsStr := details
	if len(args) > 0 {
		for _, arg := range args {
			detailsStr = strings.Replace(detailsStr, "%v", stringify(arg), 1)
		}
	}

	return NewBotError(
		ErrUserNotFound,
		"User not found",
		"Пользователь не найден.",
		detailsStr,
	)
}

func ErrPlanNotFoundf(details string, args ...interface{}) *BotError {
	detailsStr := details
	if len(args) > 0 {
		for _, arg := range args {
			detailsStr = strings.Replace(detailsStr, "%v", stringify(arg), 1)
		}
	}

	return NewBotError(
		ErrPlanNotFound,
		"Plan not found",
		"Тариф не найден или недоступен.",
		detailsStr,
	)
}

func ErrPaymentf(details string, args ...interface{}) *BotError {
	detailsStr := details
	if len(args) > 0 {
		for _, arg := range args {
			detailsStr = strings.Replace(detailsStr, "%v", stringify(arg), 1)
		}
	}

	return NewBotError(
		ErrPaymentError,
		"Payment processing failed",
		"Ошибка обработки платежа. Обратитесь к администратору.",
		detailsStr,
	)
}

func ErrSubscriptionf(details string, args ...interface{}) *BotError {
	detailsStr := details
	if len(args) > 0 {
		for _, arg := range args {
			detailsStr = strings.Replace(detailsStr, "%v", stringify(arg), 1)
		}
	}

	return NewBotError(
		ErrSubscriptionError,
		"Subscription operation failed",
		"Ошибка управления подпиской. Обратитесь к администратору.",
		detailsStr,
	)
}

func ErrConfigf(details string, args ...interface{}) *BotError {
	detailsStr := details
	if len(args) > 0 {
		for _, arg := range args {
			detailsStr = strings.Replace(detailsStr, "%v", stringify(arg), 1)
		}
	}

	return NewBotError(
		ErrConfigError,
		"Configuration error",
		"Ошибка конфигурации системы.",
		detailsStr,
	)
}

func ErrNetworkf(details string, args ...interface{}) *BotError {
	detailsStr := details
	if len(args) > 0 {
		for _, arg := range args {
			detailsStr = strings.Replace(detailsStr, "%v", stringify(arg), 1)
		}
	}

	return NewBotError(
		ErrNetworkError,
		"Network operation failed",
		"Ошибка сети. Попробуйте позже.",
		detailsStr,
	)
}

func ErrValidationf(details string, args ...interface{}) *BotError {
	detailsStr := details
	if len(args) > 0 {
		for _, arg := range args {
			detailsStr = strings.Replace(detailsStr, "%v", stringify(arg), 1)
		}
	}

	return NewBotError(
		ErrValidationError,
		"Validation failed",
		"Данные не прошли проверку.",
		detailsStr,
	)
}
