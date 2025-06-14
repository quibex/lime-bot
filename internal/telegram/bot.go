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

type Service struct {
	bot  *tgbotapi.BotAPI
	repo *db.Repository
	cfg  *config.Config
}

func New(cfg *config.Config, repo *db.Repository) (*Service, error) {
	bot, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		return nil, err
	}
	bot.Debug = false

	// Удаляем webhook чтобы использовать long-polling
	_, err = bot.Request(tgbotapi.DeleteWebhookConfig{})
	if err != nil {
		slog.Warn("Не удалось удалить webhook", "error", err)
	} else {
		slog.Info("Webhook удален, переключились на long-polling")
	}

	slog.Info("Авторизован как телеграм бот", "username", bot.Self.UserName)

	service := &Service{bot: bot, repo: repo, cfg: cfg}

	// Устанавливаем меню команд
	err = service.setCommands()
	if err != nil {
		slog.Warn("Не удалось установить меню команд", "error", err)
	}

	return service, nil
}

func (s *Service) Start(ctx context.Context) error {
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
		user := &db.User{
			TgID:     upd.Message.From.ID,
			Username: upd.Message.From.UserName,
		}
		s.repo.DB().FirstOrCreate(user, "tg_id = ?", upd.Message.From.ID)

		if upd.Message.IsCommand() {
			s.handleCommand(upd.Message)
		} else {
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

	if strings.HasPrefix(data, CallbackBuyPlan.String()) ||
		strings.HasPrefix(data, CallbackBuyPlatform.String()) ||
		strings.HasPrefix(data, CallbackBuyQty.String()) ||
		strings.HasPrefix(data, CallbackBuyMethod.String()) {
		s.handleBuyCallback(callback)
		return
	}

	if strings.HasPrefix(data, CallbackSubPlatform.String()) {
		s.handleSubscriptionCallback(callback)
		return
	}

	if data == CallbackAdminList.String() ||
		data == CallbackAdminAdd.String() ||
		data == CallbackAdminDisable.String() ||
		data == CallbackAdminCashier.String() ||
		strings.HasPrefix(data, CallbackPaymentApprove.String()) ||
		strings.HasPrefix(data, CallbackPaymentReject.String()) ||
		strings.HasPrefix(data, CallbackInfoUser.String()) ||
		strings.HasPrefix(data, CallbackDisableAdmin.String()) ||
		strings.HasPrefix(data, CallbackSetCashier.String()) {
		s.handleAdminCallback(callback)
		return
	}

	if strings.HasPrefix(data, CallbackArchivePlan.String()) {
		planIDStr := strings.TrimPrefix(data, CallbackArchivePlan.String())
		planID, err := strconv.ParseUint(planIDStr, 10, 32)
		if err != nil {
			s.answerCallback(callback.ID, "Неверный ID тарифа")
			return
		}

		result := s.repo.DB().Model(&db.Plan{}).Where("id = ?", planID).Update("archived", true)
		if result.Error != nil {
			s.answerCallback(callback.ID, "Ошибка архивирования")
			return
		}

		s.answerCallback(callback.ID, "Тариф архивирован")

		editMsg := tgbotapi.NewEditMessageText(
			callback.Message.Chat.ID,
			callback.Message.MessageID,
			"✅ Тариф архивирован",
		)
		s.bot.Send(editMsg)
		return
	}

	if strings.HasPrefix(data, CallbackArchiveMethod.String()) {
		methodIDStr := strings.TrimPrefix(data, CallbackArchiveMethod.String())
		methodID, err := strconv.ParseUint(methodIDStr, 10, 32)
		if err != nil {
			s.answerCallback(callback.ID, "Неверный ID метода")
			return
		}

		result := s.repo.DB().Model(&db.PaymentMethod{}).Where("id = ?", methodID).Update("archived", true)
		if result.Error != nil {
			s.answerCallback(callback.ID, "Ошибка архивирования")
			return
		}

		s.answerCallback(callback.ID, "Способ оплаты архивирован")

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
	cmd := Command(msg.Command())

	// Проверяем валидность команды
	if !cmd.IsValid() {
		s.handleUnknown(msg)
		return
	}

	// Проверяем права для админских команд
	if cmd.IsAdminOnly() && !s.isAdmin(msg.From.ID) {
		s.reply(msg.Chat.ID, "У вас нет прав для этой команды")
		return
	}

	switch cmd {
	case CmdStart:
		s.handleStartWithRef(msg)
	case CmdHelp:
		s.handleHelp(msg)
	case CmdPlans:
		s.handlePlans(msg)
	case CmdAddPlan:
		s.handleAddPlan(msg)
	case CmdArchivePlan:
		s.handleArchivePlan(msg)
	case CmdAddPMethod:
		s.handleAddPaymentMethod(msg)
	case CmdListPMethods:
		s.handleListPaymentMethods(msg)
	case CmdArchivePMethod:
		s.handleArchivePaymentMethod(msg)
	case CmdBuy:
		s.handleBuy(msg)
	case CmdMyKeys:
		s.handleMyKeys(msg)
	case CmdDisable:
		s.handleDisable(msg)
	case CmdEnable:
		s.handleEnable(msg)
	case CmdAdmins:
		s.handleAdmins(msg)
	case CmdPayQueue:
		s.handlePayQueue(msg)
	case CmdInfo:
		s.handleInfo(msg)
	case CmdAddAdmin:
		s.handleAddAdmin(msg)
	case CmdRef:
		s.handleRef(msg)
	case CmdFeedback:
		s.handleFeedback(msg)
	case CmdSupport:
		s.handleSupport(msg)
	}
}

func (s *Service) handleStart(msg *tgbotapi.Message) {
	// Создаем или обновляем пользователя в БД
	user := &db.User{
		TgID:     msg.From.ID,
		Username: msg.From.UserName,
	}
	s.repo.DB().FirstOrCreate(user, "tg_id = ?", msg.From.ID)

	// Обновляем username если он изменился
	if user.Username != msg.From.UserName {
		user.Username = msg.From.UserName
		s.repo.DB().Save(user)
	}

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
/support - служба поддержки
/help - справка`

	if s.isAdmin(msg.From.ID) {
		text += `

⚡ Администраторские команды:
/addplan - добавить тариф
/archiveplan - архивировать тариф
/addpmethod - добавить способ оплаты
/listpmethods - список способов оплаты
/archivepmethod - архивировать способ оплаты
/disable <username> - отключить пользователя
/enable <username> - включить пользователя
/payqueue - очередь платежей
/info <username> - информация о пользователе`

		if s.isSuperAdmin(msg.From.ID) {
			text += `

👑 Команды суперадмина:
/admins - управление админами
/add_admin @username role - добавить админа`
		}
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

func (s *Service) handleSupport(msg *tgbotapi.Message) {
	var admins []db.Admin
	result := s.repo.DB().Where("role = ? AND disabled = false", RoleSupport.String()).Find(&admins)
	if result.Error != nil || len(admins) == 0 {
		s.reply(msg.Chat.ID, "Служба поддержки временно недоступна. Попробуйте позже.")
		return
	}

	text := "🎧 Служба поддержки Lime VPN\n\nНапишите одному из наших специалистов:\n\n"
	for _, admin := range admins {
		var user db.User
		if err := s.repo.DB().First(&user, "tg_id = ?", admin.TgID).Error; err == nil {
			text += fmt.Sprintf("• @%s\n", user.Username)
		}
	}

	text += "\nОни помогут решить любые вопросы по использованию VPN!"
	s.reply(msg.Chat.ID, text)
}

func (s *Service) handleUnknown(msg *tgbotapi.Message) {
	s.reply(msg.Chat.ID, "Неизвестная команда. Используйте /help")
}

func (s *Service) reply(chatID int64, text string) error {
	// Добавляем информацию о поддержке для обычных пользователей
	if !s.isAdmin(chatID) {
		if supportInfo := s.getSupportUsers(); supportInfo != "" {
			text += supportInfo
		}
	}

	msg := tgbotapi.NewMessage(chatID, text)
	_, err := s.bot.Send(msg)
	return err
}

func (s *Service) isAdmin(userID int64) bool {
	if superAdminID, err := strconv.ParseInt(s.cfg.SuperAdminID, 10, 64); err == nil && superAdminID == userID {
		return true
	}

	var admin db.Admin
	result := s.repo.DB().Where("tg_id = ? AND disabled = false", userID).First(&admin)
	return result.Error == nil
}

func (s *Service) isSuperAdmin(userID int64) bool {
	if superAdminID, err := strconv.ParseInt(s.cfg.SuperAdminID, 10, 64); err == nil && superAdminID == userID {
		return true
	}

	var admin db.Admin
	result := s.repo.DB().Where("tg_id = ? AND disabled = false", userID).First(&admin)
	if result.Error != nil {
		return false
	}

	return AdminRole(admin.Role).CanManageAdmins()
}

func (s *Service) getSupportUsers() string {
	var admins []db.Admin
	result := s.repo.DB().Where("role = ? AND disabled = false", RoleSupport.String()).Find(&admins)
	if result.Error != nil || len(admins) == 0 {
		return ""
	}

	var supportUsers []string
	for _, admin := range admins {
		var user db.User
		if err := s.repo.DB().First(&user, "tg_id = ?", admin.TgID).Error; err == nil {
			supportUsers = append(supportUsers, "@"+user.Username)
		}
	}

	if len(supportUsers) == 0 {
		return ""
	}

	return fmt.Sprintf("\n\n💬 Нужна помощь? Напишите в поддержку: %s", strings.Join(supportUsers, ", "))
}

func (s *Service) answerCallback(callbackID, text string) {
	callback := tgbotapi.NewCallback(callbackID, text)
	s.bot.Request(callback)
}

func (s *Service) Bot() *tgbotapi.BotAPI {
	return s.bot
}

func (s *Service) setCommands() error {
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "🚀 Начать работу"},
		{Command: "help", Description: "❓ Справка"},
		{Command: "plans", Description: "📋 Список тарифов"},
		{Command: "buy", Description: "💳 Купить подписку"},
		{Command: "mykeys", Description: "🔑 Мои ключи"},
		{Command: "ref", Description: "👥 Реферальная ссылка"},
		{Command: "feedback", Description: "💬 Отправить отзыв"},
		{Command: "support", Description: "🎧 Служба поддержки"},
	}

	config := tgbotapi.NewSetMyCommands(commands...)
	_, err := s.bot.Request(config)
	return err
}
