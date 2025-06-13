package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"lime-bot/internal/config"
	"lime-bot/internal/db"
)

// Service инкапсулирует всю логику Telegram-бота.
type Service struct {
	bot  *tgbotapi.BotAPI
	repo *db.Repository
	cfg  *config.Config
}

// New создаёт Telegram-сервис.
func New(cfg *config.Config, repo *db.Repository) (*Service, error) {
	bot, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		return nil, err
	}
	bot.Debug = false
	slog.Info("Авторизован как телеграм бот", "username", bot.Self.UserName)
	return &Service{bot: bot, repo: repo, cfg: cfg}, nil
}

// Start запускает цикл обработки апдейтов Telegram.
func (s *Service) Start(ctx context.Context) error {
	// Настраиваем получение обновлений.
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := s.bot.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case upd := <-updates:
			s.handleUpdate(upd)
		}
	}
}

func (s *Service) handleUpdate(upd tgbotapi.Update) {
	if upd.Message != nil {
		// Регистрируем пользователя
		user := &db.User{
			TgID:     upd.Message.From.ID,
			Username: upd.Message.From.UserName,
		}
		s.repo.DB().FirstOrCreate(user, "tg_id = ?", upd.Message.From.ID)

		if upd.Message.IsCommand() {
			s.handleCommand(upd.Message)
		} else {
			// Проверяем, не ожидается ли отзыв
			s.handleFeedbackMessage(upd.Message)
		}
		return
	}

	if upd.CallbackQuery != nil {
		s.handleCallbackQuery(upd.CallbackQuery)
		return
	}
}

func (s *Service) handleCallbackQuery(callback *tgbotapi.CallbackQuery) {
	data := callback.Data

	// Обработка callback'ов для покупки
	if strings.HasPrefix(data, "buy_") {
		s.handleBuyCallback(callback)
		return
	}

	// Обработка callback'ов для подписок
	if strings.HasPrefix(data, "sub_") {
		s.handleSubscriptionCallback(callback)
		return
	}

	// Обработка callback'ов для административных функций
	if strings.HasPrefix(data, "admin_") || strings.HasPrefix(data, "payment_") || strings.HasPrefix(data, "info_user_") {
		s.handleAdminCallback(callback)
		return
	}

	// Обработка callback'ов для архивирования тарифов
	if strings.HasPrefix(data, "archive_plan_") {
		planIDStr := strings.TrimPrefix(data, "archive_plan_")
		planID, err := strconv.ParseUint(planIDStr, 10, 32)
		if err != nil {
			s.answerCallback(callback.ID, "Неверный ID тарифа")
			return
		}

		// Архивируем тариф
		result := s.repo.DB().Model(&db.Plan{}).Where("id = ?", planID).Update("archived", true)
		if result.Error != nil {
			s.answerCallback(callback.ID, "Ошибка архивирования")
			return
		}

		s.answerCallback(callback.ID, "Тариф архивирован")

		// Обновляем сообщение
		editMsg := tgbotapi.NewEditMessageText(
			callback.Message.Chat.ID,
			callback.Message.MessageID,
			"✅ Тариф архивирован",
		)
		s.bot.Send(editMsg)
		return
	}

	// Обработка callback'ов для архивирования способов оплаты
	if strings.HasPrefix(data, "archive_method_") {
		methodIDStr := strings.TrimPrefix(data, "archive_method_")
		methodID, err := strconv.ParseUint(methodIDStr, 10, 32)
		if err != nil {
			s.answerCallback(callback.ID, "Неверный ID метода")
			return
		}

		// Архивируем способ оплаты
		result := s.repo.DB().Model(&db.PaymentMethod{}).Where("id = ?", methodID).Update("archived", true)
		if result.Error != nil {
			s.answerCallback(callback.ID, "Ошибка архивирования")
			return
		}

		s.answerCallback(callback.ID, "Способ оплаты архивирован")

		// Обновляем сообщение
		editMsg := tgbotapi.NewEditMessageText(
			callback.Message.Chat.ID,
			callback.Message.MessageID,
			"✅ Способ оплаты архивирован",
		)
		s.bot.Send(editMsg)
		return
	}
}

func (s *Service) handleCommand(msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start":
		s.handleStartWithRef(msg)
	case "help":
		s.handleHelp(msg)
	case "plans":
		s.handlePlans(msg)
	case "addplan":
		if s.isAdmin(msg.From.ID) {
			s.handleAddPlan(msg)
		} else {
			s.reply(msg.Chat.ID, "У вас нет прав для этой команды")
		}
	case "archiveplan":
		if s.isAdmin(msg.From.ID) {
			s.handleArchivePlan(msg)
		} else {
			s.reply(msg.Chat.ID, "У вас нет прав для этой команды")
		}
	case "addpmethod":
		if s.isAdmin(msg.From.ID) {
			s.handleAddPaymentMethod(msg)
		} else {
			s.reply(msg.Chat.ID, "У вас нет прав для этой команды")
		}
	case "listpmethods":
		if s.isAdmin(msg.From.ID) {
			s.handleListPaymentMethods(msg)
		} else {
			s.reply(msg.Chat.ID, "У вас нет прав для этой команды")
		}
	case "archivepmethod":
		if s.isAdmin(msg.From.ID) {
			s.handleArchivePaymentMethod(msg)
		} else {
			s.reply(msg.Chat.ID, "У вас нет прав для этой команды")
		}
	case "buy":
		s.handleBuy(msg)
	case "mykeys":
		s.handleMyKeys(msg)
	case "disable":
		s.handleDisable(msg)
	case "enable":
		s.handleEnable(msg)
	case "admins":
		s.handleAdmins(msg)
	case "payqueue":
		s.handlePayQueue(msg)
	case "info":
		s.handleInfo(msg)
	case "ref":
		s.handleRef(msg)
	case "feedback":
		s.handleFeedback(msg)
	default:
		s.handleUnknown(msg)
	}
}

func (s *Service) handleStart(msg *tgbotapi.Message) {
	text := `Добро пожаловать в Lime VPN! 🍋

Доступные команды:
/plans - посмотреть тарифы
/help - справка`
	s.reply(msg.Chat.ID, text)
}

func (s *Service) handleHelp(msg *tgbotapi.Message) {
	text := `🍋 Lime VPN - Быстрый и надежный VPN

👤 Команды пользователя:
/plans - список тарифов
/buy - купить подписку
/mykeys - мои ключи
/ref - реферальная ссылка
/feedback - отправить отзыв
/help - справка`

	if s.isAdmin(msg.From.ID) {
		text += `

👑 Администраторские команды:
/addplan - добавить тариф
/archiveplan - архивировать тариф
/addpmethod - добавить способ оплаты
/listpmethods - список способов оплаты
/archivepmethod - архивировать способ оплаты
/disable <username> - отключить пользователя
/enable <username> - включить пользователя
/admins - управление админами
/payqueue - очередь платежей
/info <username> - информация о пользователе`
	}

	s.reply(msg.Chat.ID, text)
}

func (s *Service) handlePlans(msg *tgbotapi.Message) {
	var plans []db.Plan
	result := s.repo.DB().Where("archived = false").Find(&plans)
	if result.Error != nil {
		s.reply(msg.Chat.ID, "Ошибка получения тарифов")
		return
	}

	if len(plans) == 0 {
		s.reply(msg.Chat.ID, "Тарифы пока не добавлены")
		return
	}

	text := "📋 Доступные тарифы:\n\n"
	for _, plan := range plans {
		text += fmt.Sprintf("🔹 %s\n💰 %d руб.\n⏱ %d дней\n\n",
			plan.Name, plan.PriceInt, plan.DurationDays)
	}
	s.reply(msg.Chat.ID, text)
}

func (s *Service) handleAddPlan(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) < 3 {
		s.reply(msg.Chat.ID, "Использование: /addplan <название> <цена> <дни>\nПример: /addplan Месяц 200 30")
		return
	}

	name := args[0]
	price, err := strconv.Atoi(args[1])
	if err != nil {
		s.reply(msg.Chat.ID, "Неверная цена")
		return
	}

	days, err := strconv.Atoi(args[2])
	if err != nil {
		s.reply(msg.Chat.ID, "Неверное количество дней")
		return
	}

	plan := &db.Plan{
		Name:         name,
		PriceInt:     price,
		DurationDays: days,
	}

	result := s.repo.DB().Create(plan)
	if result.Error != nil {
		s.reply(msg.Chat.ID, "Ошибка создания тарифа")
		return
	}

	s.reply(msg.Chat.ID, fmt.Sprintf("✅ Тариф \"%s\" создан", name))
}

func (s *Service) handleArchivePlan(msg *tgbotapi.Message) {
	var plans []db.Plan
	result := s.repo.DB().Where("archived = false").Find(&plans)
	if result.Error != nil {
		s.reply(msg.Chat.ID, "Ошибка получения тарифов")
		return
	}

	if len(plans) == 0 {
		s.reply(msg.Chat.ID, "Нет активных тарифов")
		return
	}

	// Создаем inline клавиатуру
	var keyboard [][]tgbotapi.InlineKeyboardButton
	for _, plan := range plans {
		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%s (%d руб.)", plan.Name, plan.PriceInt),
			fmt.Sprintf("archive_plan_%d", plan.ID),
		)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
	}

	msgConfig := tgbotapi.NewMessage(msg.Chat.ID, "Выберите тариф для архивирования:")
	msgConfig.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	s.bot.Send(msgConfig)
}

func (s *Service) handleUnknown(msg *tgbotapi.Message) {
	s.reply(msg.Chat.ID, "Неизвестная команда. Используйте /help")
}

func (s *Service) reply(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := s.bot.Send(msg)
	return err
}

func (s *Service) isAdmin(userID int64) bool {
	// Проверка супер-админа
	if superAdminID, err := strconv.ParseInt(s.cfg.SuperAdminID, 10, 64); err == nil && superAdminID == userID {
		return true
	}

	// Проверка в БД
	var admin db.Admin
	result := s.repo.DB().Where("tg_id = ? AND disabled = false", userID).First(&admin)
	return result.Error == nil
}

func (s *Service) answerCallback(callbackID, text string) {
	callback := tgbotapi.NewCallback(callbackID, text)
	s.bot.Request(callback)
}

// Bot возвращает экземпляр Telegram бота
func (s *Service) Bot() *tgbotapi.BotAPI {
	return s.bot
}
