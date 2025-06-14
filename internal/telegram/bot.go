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
	slog.Info("Creating Telegram bot service", "bot_token_length", len(cfg.BotToken))

	if cfg.BotToken == "" {
		return nil, ErrConfigf("Bot token is empty")
	}

	bot, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		return nil, ErrNetworkf("Failed to create bot API: %v", err)
	}
	bot.Debug = false

	slog.Info("Bot API created successfully", "bot_username", bot.Self.UserName)

	// Удаляем webhook чтобы использовать long-polling
	_, err = bot.Request(tgbotapi.DeleteWebhookConfig{})
	if err != nil {
		slog.Warn("Failed to delete webhook", "error", err)
	} else {
		slog.Info("Webhook deleted, switched to long-polling")
	}

	slog.Info("Authorized as telegram bot", "username", bot.Self.UserName)

	service := &Service{bot: bot, repo: repo, cfg: cfg}

	// Устанавливаем меню команд
	if err := service.setCommands(); err != nil {
		slog.Warn("Failed to set command menu", "error", err)
	} else {
		slog.Info("Command menu set successfully")
	}

	return service, nil
}

func (s *Service) Start(ctx context.Context) error {
	slog.Info("Starting Telegram bot service")

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := s.bot.GetUpdatesChan(u)
	slog.Info("Listening for Telegram updates")

	for {
		select {
		case <-ctx.Done():
			slog.Info("Bot service stopped by context")
			return ctx.Err()
		case upd := <-updates:
			s.handleUpdate(upd)
		}
	}
}

func (s *Service) handleUpdate(upd tgbotapi.Update) {
	if upd.Message != nil {
		slog.Debug("Received message",
			"user_id", upd.Message.From.ID,
			"username", upd.Message.From.UserName,
			"chat_id", upd.Message.Chat.ID,
			"is_command", upd.Message.IsCommand(),
		)

		// Создаем или обновляем пользователя в БД
		user := &db.User{
			TgID:     upd.Message.From.ID,
			Username: upd.Message.From.UserName,
		}

		result := s.repo.DB().FirstOrCreate(user, "tg_id = ?", upd.Message.From.ID)
		if result.Error != nil {
			s.logAndReportError("User creation/update failed", result.Error, map[string]interface{}{
				"user_id":  upd.Message.From.ID,
				"username": upd.Message.From.UserName,
			})
		} else if result.RowsAffected > 0 {
			slog.Info("New user registered", "user_id", upd.Message.From.ID, "username", upd.Message.From.UserName)
		}

		// Обновляем username если он изменился
		if user.Username != upd.Message.From.UserName {
			user.Username = upd.Message.From.UserName
			if err := s.repo.DB().Save(user).Error; err != nil {
				s.logAndReportError("Username update failed", err, map[string]interface{}{
					"user_id":      upd.Message.From.ID,
					"old_username": user.Username,
					"new_username": upd.Message.From.UserName,
				})
			}
		}

		if upd.Message.IsCommand() {
			s.handleCommand(upd.Message)
		} else {
			s.handleFeedbackMessage(upd.Message)
		}
		return
	}

	if upd.CallbackQuery != nil {
		slog.Debug("Received callback query",
			"user_id", upd.CallbackQuery.From.ID,
			"data", upd.CallbackQuery.Data,
		)
		s.handleCallbackQuery(upd.CallbackQuery)
		return
	}
}

func (s *Service) handleCallbackQuery(callback *tgbotapi.CallbackQuery) {
	data := callback.Data
	slog.Info("Processing callback", "data", data, "user_id", callback.From.ID)

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
			s.logAndReportError("Invalid plan ID for archive", ErrValidationf("Invalid plan ID: %v", planIDStr), map[string]interface{}{
				"plan_id_str": planIDStr,
				"user_id":     callback.From.ID,
			})
			s.answerCallback(callback.ID, "Неверный ID тарифа")
			return
		}

		slog.Info("Archiving plan", "plan_id", planID, "admin_id", callback.From.ID)

		result := s.repo.DB().Model(&db.Plan{}).Where("id = ?", planID).Update("archived", true)
		if result.Error != nil {
			s.logAndReportError("Plan archive failed", result.Error, map[string]interface{}{
				"plan_id":  planID,
				"admin_id": callback.From.ID,
			})
			s.answerCallback(callback.ID, "Ошибка архивирования")
			return
		}

		slog.Info("Plan archived successfully", "plan_id", planID, "admin_id", callback.From.ID)
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
			s.logAndReportError("Invalid method ID for archive", ErrValidationf("Invalid method ID: %v", methodIDStr), map[string]interface{}{
				"method_id_str": methodIDStr,
				"user_id":       callback.From.ID,
			})
			s.answerCallback(callback.ID, "Неверный ID метода")
			return
		}

		slog.Info("Archiving payment method", "method_id", methodID, "admin_id", callback.From.ID)

		result := s.repo.DB().Model(&db.PaymentMethod{}).Where("id = ?", methodID).Update("archived", true)
		if result.Error != nil {
			s.logAndReportError("Payment method archive failed", result.Error, map[string]interface{}{
				"method_id": methodID,
				"admin_id":  callback.From.ID,
			})
			s.answerCallback(callback.ID, "Ошибка архивирования")
			return
		}

		slog.Info("Payment method archived successfully", "method_id", methodID, "admin_id", callback.From.ID)
		s.answerCallback(callback.ID, "Способ оплаты архивирован")

		editMsg := tgbotapi.NewEditMessageText(
			callback.Message.Chat.ID,
			callback.Message.MessageID,
			"✅ Способ оплаты архивирован",
		)
		s.bot.Send(editMsg)
		return
	}

	slog.Warn("Unknown callback data received", "data", data, "user_id", callback.From.ID)
}

func (s *Service) handleCommand(msg *tgbotapi.Message) {
	cmd := Command(msg.Command())
	slog.Info("Command received", "command", cmd, "user_id", msg.From.ID, "username", msg.From.UserName)

	// Проверяем валидность команды
	if !cmd.IsValid() {
		slog.Warn("Invalid command received", "command", cmd, "user_id", msg.From.ID)
		s.handleUnknown(msg)
		return
	}

	// Проверяем права для админских команд
	if cmd.IsAdminOnly() && !s.isAdmin(msg.From.ID) {
		s.logAndReportError("Unauthorized admin command", ErrPermission("Non-admin user attempted admin command"), map[string]interface{}{
			"command":  string(cmd),
			"user_id":  msg.From.ID,
			"username": msg.From.UserName,
		})
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
