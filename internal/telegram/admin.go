package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"lime-bot/internal/db"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (s *Service) handleAdmins(msg *tgbotapi.Message) {
	if !s.isAdmin(msg.From.ID) {
		s.reply(msg.Chat.ID, "У вас нет прав для этой команды")
		return
	}

	
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("➕ Добавить админа", "admin_add")},
		{tgbotapi.NewInlineKeyboardButtonData("📋 Список админов", "admin_list")},
		{tgbotapi.NewInlineKeyboardButtonData("🗑 Отключить админа", "admin_disable")},
		{tgbotapi.NewInlineKeyboardButtonData("⭐ Назначить кассира", "admin_cashier")},
	}

	msgConfig := tgbotapi.NewMessage(msg.Chat.ID, "Управление администраторами:")
	msgConfig.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	s.bot.Send(msgConfig)
}

func (s *Service) handlePayQueue(msg *tgbotapi.Message) {
	if !s.isAdmin(msg.From.ID) {
		s.reply(msg.Chat.ID, "У вас нет прав для этой команды")
		return
	}

	
	var payments []db.Payment
	result := s.repo.DB().Where("status = 'pending'").
		Preload("User").
		Preload("Plan").
		Preload("Method").
		Order("created_at ASC").
		Find(&payments)

	if result.Error != nil {
		s.reply(msg.Chat.ID, "Ошибка получения очереди платежей")
		return
	}

	if len(payments) == 0 {
		s.reply(msg.Chat.ID, "Очередь платежей пуста")
		return
	}

	text := "💳 Очередь платежей на проверку:\n\n"
	var keyboard [][]tgbotapi.InlineKeyboardButton

	for i, payment := range payments {
		text += fmt.Sprintf("🆔 #%d\n👤 @%s\n💰 %d руб.\n📦 %s x%d\n💳 %s (%s)\n📅 %s\n\n",
			payment.ID,
			payment.User.Username,
			payment.Amount,
			payment.Plan.Name,
			payment.Qty,
			payment.Method.Bank,
			payment.Method.PhoneNumber,
			payment.CreatedAt.Format("02.01.2006 15:04"),
		)

		
		buttonRow := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("✅ #%d", payment.ID),
				fmt.Sprintf("payment_approve_%d", payment.ID),
			),
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("❌ #%d", payment.ID),
				fmt.Sprintf("payment_reject_%d", payment.ID),
			),
		}
		keyboard = append(keyboard, buttonRow)

		
		if i >= 4 {
			text += "...и еще платежи\n"
			break
		}
	}

	msgConfig := tgbotapi.NewMessage(msg.Chat.ID, text)
	if len(keyboard) > 0 {
		msgConfig.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	}
	s.bot.Send(msgConfig)
}

func (s *Service) handleInfo(msg *tgbotapi.Message) {
	if !s.isAdmin(msg.From.ID) {
		s.reply(msg.Chat.ID, "У вас нет прав для этой команды")
		return
	}

	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		s.reply(msg.Chat.ID, "Использование: /info <username>\nПример: /info john_doe")
		return
	}

	username := args[0]

	
	var users []db.User
	result := s.repo.DB().Where("username LIKE ?", "%"+username+"%").Limit(5).Find(&users)
	if result.Error != nil {
		s.reply(msg.Chat.ID, "Ошибка поиска пользователей")
		return
	}

	if len(users) == 0 {
		s.reply(msg.Chat.ID, "Пользователи не найдены")
		return
	}

	if len(users) > 1 {
		
		text := "Найдено несколько пользователей:\n\n"
		var keyboard [][]tgbotapi.InlineKeyboardButton

		for _, user := range users {
			text += fmt.Sprintf("👤 @%s (ID: %d)\n", user.Username, user.TgID)
			btn := tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("ℹ️ @%s", user.Username),
				fmt.Sprintf("info_user_%d", user.TgID),
			)
			keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
		}

		msgConfig := tgbotapi.NewMessage(msg.Chat.ID, text)
		msgConfig.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboard...)
		s.bot.Send(msgConfig)
		return
	}

	
	s.sendUserInfo(msg.Chat.ID, users[0].TgID)
}

func (s *Service) sendUserInfo(chatID int64, userID int64) {
	
	var user db.User
	result := s.repo.DB().First(&user, "tg_id = ?", userID)
	if result.Error != nil {
		s.reply(chatID, "Пользователь не найден")
		return
	}

	
	var subscriptions []db.Subscription
	s.repo.DB().Where("user_id = ?", userID).
		Preload("Plan").
		Order("end_date DESC").
		Find(&subscriptions)

	
	var payments []db.Payment
	s.repo.DB().Where("user_id = ?", userID).
		Preload("Plan").
		Preload("Method").
		Order("created_at DESC").
		Limit(5).
		Find(&payments)

	text := fmt.Sprintf(`👤 Информация о пользователе:

🆔 ID: %d
👤 Username: @%s
📞 Телефон: %s
🔗 Реф. код: %s
📅 Регистрация: %s

🔑 Подписки (%d):`,
		user.TgID,
		user.Username,
		user.Phone,
		user.RefCode,
		user.CreatedAt.Format("02.01.2006"),
		len(subscriptions),
	)

	
	for _, sub := range subscriptions {
		status := "🟢"
		if !sub.Active {
			status = "🔴"
		}
		if time.Now().After(sub.EndDate) {
			status = "⏰"
		}

		text += fmt.Sprintf("\n%s %s (%s) до %s",
			status, sub.Plan.Name, sub.Platform, sub.EndDate.Format("02.01.2006"))
	}

	text += fmt.Sprintf("\n\n💳 Последние платежи (%d):", len(payments))

	
	for _, payment := range payments {
		statusEmoji := "⏳"
		switch payment.Status {
		case "approved":
			statusEmoji = "✅"
		case "rejected":
			statusEmoji = "❌"
		}

		text += fmt.Sprintf("\n%s %d руб. (%s) - %s",
			statusEmoji, payment.Amount, payment.Plan.Name, payment.CreatedAt.Format("02.01"))
	}

	s.reply(chatID, text)
}

func (s *Service) handleAdminCallback(callback *tgbotapi.CallbackQuery) {
	data := callback.Data

	if strings.HasPrefix(data, "admin_") {
		s.handleAdminManagementCallback(callback)
		return
	}

	if strings.HasPrefix(data, "payment_") {
		s.handlePaymentCallback(callback)
		return
	}

	if strings.HasPrefix(data, "info_user_") {
		userIDStr := strings.TrimPrefix(data, "info_user_")
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			s.answerCallback(callback.ID, "Неверный ID пользователя")
			return
		}

		
		s.sendUserInfo(callback.Message.Chat.ID, userID)
		s.answerCallback(callback.ID, "")
		return
	}
}

func (s *Service) handleAdminManagementCallback(callback *tgbotapi.CallbackQuery) {
	data := callback.Data

	switch data {
	case "admin_list":
		s.showAdminList(callback)
	case "admin_add":
		
		s.answerCallback(callback.ID, "Функция в разработке")
	case "admin_disable":
		
		s.answerCallback(callback.ID, "Функция в разработке")
	case "admin_cashier":
		
		s.answerCallback(callback.ID, "Функция в разработке")
	}
}

func (s *Service) showAdminList(callback *tgbotapi.CallbackQuery) {
	var admins []db.Admin
	result := s.repo.DB().Where("disabled = false").Find(&admins)
	if result.Error != nil {
		s.answerCallback(callback.ID, "Ошибка получения списка")
		return
	}

	text := "👥 Список администраторов:\n\n"
	for _, admin := range admins {
		var user db.User
		s.repo.DB().First(&user, "tg_id = ?", admin.TgID)

		roleEmoji := "👤"
		switch admin.Role {
		case "super":
			roleEmoji = "👑"
		case "cashier":
			roleEmoji = "💰"
		case "support":
			roleEmoji = "🎧"
		}

		text += fmt.Sprintf("%s @%s (%s)\n", roleEmoji, user.Username, admin.Role)
	}

	editMsg := tgbotapi.NewEditMessageText(
		callback.Message.Chat.ID,
		callback.Message.MessageID,
		text,
	)
	s.bot.Send(editMsg)
	s.answerCallback(callback.ID, "")
}

func (s *Service) handlePaymentCallback(callback *tgbotapi.CallbackQuery) {
	data := callback.Data

	if strings.HasPrefix(data, "payment_approve_") {
		paymentIDStr := strings.TrimPrefix(data, "payment_approve_")
		paymentID, err := strconv.ParseUint(paymentIDStr, 10, 32)
		if err != nil {
			s.answerCallback(callback.ID, "Неверный ID платежа")
			return
		}

		err = s.approvePayment(uint(paymentID), callback.From.ID)
		if err != nil {
			s.answerCallback(callback.ID, fmt.Sprintf("Ошибка: %v", err))
			return
		}

		s.answerCallback(callback.ID, "✅ Платеж одобрен")

		
		editMsg := tgbotapi.NewEditMessageText(
			callback.Message.Chat.ID,
			callback.Message.MessageID,
			"✅ Платеж одобрен",
		)
		s.bot.Send(editMsg)
		return
	}

	if strings.HasPrefix(data, "payment_reject_") {
		paymentIDStr := strings.TrimPrefix(data, "payment_reject_")
		paymentID, err := strconv.ParseUint(paymentIDStr, 10, 32)
		if err != nil {
			s.answerCallback(callback.ID, "Неверный ID платежа")
			return
		}

		err = s.rejectPayment(uint(paymentID), callback.From.ID)
		if err != nil {
			s.answerCallback(callback.ID, fmt.Sprintf("Ошибка: %v", err))
			return
		}

		s.answerCallback(callback.ID, "❌ Платеж отклонен")

		
		editMsg := tgbotapi.NewEditMessageText(
			callback.Message.Chat.ID,
			callback.Message.MessageID,
			"❌ Платеж отклонен",
		)
		s.bot.Send(editMsg)
		return
	}
}

func (s *Service) approvePayment(paymentID uint, adminID int64) error {
	
	tx := s.repo.DB().Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	
	result := tx.Model(&db.Payment{}).
		Where("id = ? AND status = 'pending'", paymentID).
		Updates(map[string]interface{}{
			"status":      "approved",
			"approved_by": adminID,
		})

	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	if result.RowsAffected == 0 {
		tx.Rollback()
		return fmt.Errorf("платеж не найден или уже обработан")
	}

	
	

	return tx.Commit().Error
}

func (s *Service) rejectPayment(paymentID uint, adminID int64) error {
	
	tx := s.repo.DB().Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	
	var payment db.Payment
	if err := tx.First(&payment, paymentID).Error; err != nil {
		tx.Rollback()
		return err
	}

	if payment.Status != "pending" {
		tx.Rollback()
		return fmt.Errorf("платеж уже обработан")
	}

	
	payment.Status = "rejected"
	payment.ApprovedBy = &adminID

	if err := tx.Save(&payment).Error; err != nil {
		tx.Rollback()
		return err
	}

	
	var subscriptions []db.Subscription
	tx.Where("payment_id = ?", paymentID).Find(&subscriptions)

	for _, sub := range subscriptions {
		
		s.disablePeer(sub.Interface, sub.PublicKey)

		
		tx.Model(&sub).Update("active", false)
	}

	return tx.Commit().Error
}
