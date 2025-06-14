package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"lime-bot/internal/db"
	"lime-bot/internal/gates/wgagent"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gorm.io/gorm"
)

func (s *Service) handleAdmins(msg *tgbotapi.Message) {
	if !s.isSuperAdmin(msg.From.ID) {
		s.reply(msg.Chat.ID, "У вас нет прав для управления администраторами")
		return
	}

	// Создаем меню управления админами
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("➕ Добавить админа", CallbackAdminAdd.String())},
		{tgbotapi.NewInlineKeyboardButtonData("📋 Список админов", CallbackAdminList.String())},
		{tgbotapi.NewInlineKeyboardButtonData("🗑 Отключить админа", CallbackAdminDisable.String())},
		{tgbotapi.NewInlineKeyboardButtonData("⭐ Назначить кассира", CallbackAdminCashier.String())},
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

		// Создаем кнопки для одобрения/отклонения
		buttonRow := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("✅ #%d", payment.ID),
				CallbackPaymentApprove.WithID(payment.ID),
			),
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("❌ #%d", payment.ID),
				CallbackPaymentReject.WithID(payment.ID),
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
				CallbackInfoUser.WithID(user.TgID),
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

	if data == CallbackAdminList.String() || data == CallbackAdminAdd.String() || data == CallbackAdminDisable.String() || data == CallbackAdminCashier.String() {
		s.handleAdminManagementCallback(callback)
		return
	}

	if strings.HasPrefix(data, CallbackPaymentApprove.String()) || strings.HasPrefix(data, CallbackPaymentReject.String()) {
		s.handlePaymentCallback(callback)
		return
	}

	if strings.HasPrefix(data, CallbackInfoUser.String()) {
		userIDStr := strings.TrimPrefix(data, CallbackInfoUser.String())
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			s.answerCallback(callback.ID, "Неверный ID пользователя")
			return
		}

		// Отправляем информацию о пользователе
		s.sendUserInfo(callback.Message.Chat.ID, userID)
		s.answerCallback(callback.ID, "")
		return
	}

	// Отключение админа по префиксу
	if strings.HasPrefix(data, CallbackDisableAdmin.String()) {
		userIDStr := strings.TrimPrefix(data, CallbackDisableAdmin.String())
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			s.answerCallback(callback.ID, "Неверный ID админа")
			return
		}

		if err := s.disableAdmin(userID); err != nil {
			s.answerCallback(callback.ID, fmt.Sprintf("Ошибка: %v", err))
			return
		}

		s.answerCallback(callback.ID, "✅ Админ отключен")
		editMsg := tgbotapi.NewEditMessageText(
			callback.Message.Chat.ID,
			callback.Message.MessageID,
			"✅ Администратор отключен",
		)
		s.bot.Send(editMsg)
		return
	}

	// Назначение кассира
	if strings.HasPrefix(data, CallbackSetCashier.String()) {
		userIDStr := strings.TrimPrefix(data, CallbackSetCashier.String())
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			s.answerCallback(callback.ID, "Неверный ID админа")
			return
		}

		if err := s.setCashierRole(userID); err != nil {
			s.answerCallback(callback.ID, fmt.Sprintf("Ошибка: %v", err))
			return
		}

		s.answerCallback(callback.ID, "⭐ Роль изменена")
		editMsg := tgbotapi.NewEditMessageText(
			callback.Message.Chat.ID,
			callback.Message.MessageID,
			"⭐ Роль кассира назначена",
		)
		s.bot.Send(editMsg)
		return
	}
}

func (s *Service) handleAdminManagementCallback(callback *tgbotapi.CallbackQuery) {
	data := CallbackData(callback.Data)

	switch data {
	case CallbackAdminList:
		s.showAdminList(callback)
	case CallbackAdminAdd:
		s.showAddAdminForm(callback)
	case CallbackAdminDisable:
		s.showDisableAdminList(callback)
	case CallbackAdminCashier:
		s.showChangeCashierList(callback)
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

		role := AdminRole(admin.Role)
		text += fmt.Sprintf("%s @%s (%s)\n", role.Emoji(), user.Username, role.DisplayName())
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

	if strings.HasPrefix(data, CallbackPaymentApprove.String()) {
		paymentIDStr := strings.TrimPrefix(data, CallbackPaymentApprove.String())
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

	if strings.HasPrefix(data, CallbackPaymentReject.String()) {
		paymentIDStr := strings.TrimPrefix(data, CallbackPaymentReject.String())
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
	// Начинаем транзакцию
	tx := s.repo.DB().Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Получаем платеж с планом
	var payment db.Payment
	if err := tx.Preload("Plan").First(&payment, paymentID).Error; err != nil {
		tx.Rollback()
		return err
	}

	if payment.Status != PaymentStatusPending.String() {
		tx.Rollback()
		return fmt.Errorf("платеж уже обработан")
	}

	// Обновляем статус платежа
	result := tx.Model(&payment).Updates(map[string]interface{}{
		"status":      PaymentStatusApproved.String(),
		"approved_by": adminID,
	})

	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}

	// Создаем подписки для каждого ключа
	for i := 0; i < payment.Qty; i++ {
		subscription, err := s.createSubscriptionForPayment(tx, &payment)
		if err != nil {
			tx.Rollback()
			return err
		}

		// Отправляем ключ пользователю
		s.sendSubscriptionToUser(payment.UserID, subscription)
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

	// Обновляем статус платежа
	payment.Status = PaymentStatusRejected.String()
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

func (s *Service) showAddAdminForm(callback *tgbotapi.CallbackQuery) {
	text := `➕ Добавление администратора

Отправьте сообщение в формате:
/add_admin @username role

Доступные роли:
• super - суперадмин
• admin - администратор
• cashier - кассир  
• support - поддержка

Пример: /add_admin @john_doe admin`

	editMsg := tgbotapi.NewEditMessageText(
		callback.Message.Chat.ID,
		callback.Message.MessageID,
		text,
	)
	s.bot.Send(editMsg)
	s.answerCallback(callback.ID, "")
}

func (s *Service) showDisableAdminList(callback *tgbotapi.CallbackQuery) {
	var admins []db.Admin
	result := s.repo.DB().Where("disabled = false").Find(&admins)
	if result.Error != nil {
		s.answerCallback(callback.ID, "Ошибка получения списка")
		return
	}

	if len(admins) <= 1 {
		s.answerCallback(callback.ID, "Нет админов для отключения")
		return
	}

	text := "🗑 Выберите администратора для отключения:\n\n"
	var keyboard [][]tgbotapi.InlineKeyboardButton

	for _, admin := range admins {
		// Не показываем текущего пользователя
		if admin.TgID == callback.From.ID {
			continue
		}

		var user db.User
		s.repo.DB().First(&user, "tg_id = ?", admin.TgID)

		role := AdminRole(admin.Role)
		text += fmt.Sprintf("%s @%s (%s)\n", role.Emoji(), user.Username, role.DisplayName())

		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("🗑 @%s", user.Username),
			CallbackDisableAdmin.WithID(admin.TgID),
		)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
	}

	editMsg := tgbotapi.NewEditMessageText(
		callback.Message.Chat.ID,
		callback.Message.MessageID,
		text,
	)
	if len(keyboard) > 0 {
		editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboard}
	}
	s.bot.Send(editMsg)
	s.answerCallback(callback.ID, "")
}

func (s *Service) showChangeCashierList(callback *tgbotapi.CallbackQuery) {
	var admins []db.Admin
	result := s.repo.DB().Where("disabled = false").Find(&admins)
	if result.Error != nil {
		s.answerCallback(callback.ID, "Ошибка получения списка")
		return
	}

	text := "⭐ Выберите администратора для назначения кассиром:\n\n"
	var keyboard [][]tgbotapi.InlineKeyboardButton

	for _, admin := range admins {
		var user db.User
		s.repo.DB().First(&user, "tg_id = ?", admin.TgID)

		role := AdminRole(admin.Role)
		text += fmt.Sprintf("%s @%s (%s)\n", role.Emoji(), user.Username, role.DisplayName())

		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("⭐ @%s", user.Username),
			CallbackSetCashier.WithID(admin.TgID),
		)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
	}

	editMsg := tgbotapi.NewEditMessageText(
		callback.Message.Chat.ID,
		callback.Message.MessageID,
		text,
	)
	if len(keyboard) > 0 {
		editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboard}
	}
	s.bot.Send(editMsg)
	s.answerCallback(callback.ID, "")
}

func (s *Service) disableAdmin(adminID int64) error {
	result := s.repo.DB().Model(&db.Admin{}).
		Where("tg_id = ?", adminID).
		Update("disabled", true)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("администратор не найден")
	}

	return nil
}

func (s *Service) setCashierRole(adminID int64) error {
	result := s.repo.DB().Model(&db.Admin{}).
		Where("tg_id = ?", adminID).
		Update("role", RoleCashier.String())

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("администратор не найден")
	}

	return nil
}

func (s *Service) handleAddAdmin(msg *tgbotapi.Message) {
	if !s.isSuperAdmin(msg.From.ID) {
		s.reply(msg.Chat.ID, "У вас нет прав для управления администраторами")
		return
	}

	args := strings.Fields(msg.CommandArguments())
	if len(args) < 2 {
		s.reply(msg.Chat.ID, `Использование: /add_admin @username role

Доступные роли:
• super - суперадмин
• admin - администратор
• cashier - кассир
• support - поддержка

Пример: /add_admin @john_doe admin`)
		return
	}

	username := strings.TrimPrefix(args[0], "@")
	role := AdminRole(args[1])

	// Проверяем валидность роли
	if !role.IsValid() {
		s.reply(msg.Chat.ID, "Неверная роль. Доступные: super, admin, cashier, support")
		return
	}

	// Находим пользователя по username
	var user db.User
	result := s.repo.DB().Where("username = ?", username).First(&user)
	if result.Error != nil {
		s.reply(msg.Chat.ID, fmt.Sprintf("Пользователь @%s не найден.\n\nПользователь должен сначала написать боту командой /start", username))
		return
	}

	// Проверяем, не является ли уже админом
	var existingAdmin db.Admin
	result = s.repo.DB().Where("tg_id = ?", user.TgID).First(&existingAdmin)
	if result.Error == nil {
		s.reply(msg.Chat.ID, "Пользователь уже является администратором")
		return
	}

	// Создаем нового админа
	admin := &db.Admin{
		TgID:     user.TgID,
		Role:     role.String(),
		Disabled: false,
	}

	result = s.repo.DB().Create(admin)
	if result.Error != nil {
		s.handleError(msg.Chat.ID, ErrDatabasef("Failed to create admin: %v", result.Error))
		return
	}

	s.reply(msg.Chat.ID, fmt.Sprintf("✅ Пользователь @%s назначен как %s", username, role.DisplayName()))
}

func (s *Service) createSubscriptionForPayment(tx *gorm.DB, payment *db.Payment) (*db.Subscription, error) {
	// Создаем конфигурацию WireGuard
	ctx := context.Background()

	wgConfig := wgagent.Config{
		Addr:     s.cfg.WGAgentAddr,
		CertFile: s.cfg.WGClientCert,
		KeyFile:  s.cfg.WGClientKey,
		CAFile:   s.cfg.WGCACert,
	}
	wgClient, err := wgagent.NewClient(wgConfig)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания WG клиента: %w", err)
	}
	defer wgClient.Close()

	// Генерируем конфигурацию пира
	peerReq := &wgagent.GeneratePeerConfigRequest{
		Interface:      "wg0",
		ServerEndpoint: s.cfg.WGServerEndpoint,
		DNSServers:     "1.1.1.1, 1.0.0.1",
		AllowedIPs:     "0.0.0.0/0",
	}

	peerResp, err := wgClient.GeneratePeerConfig(ctx, peerReq)
	if err != nil {
		return nil, fmt.Errorf("ошибка генерации конфигурации пира: %w", err)
	}

	// Добавляем пира к интерфейсу
	peerID := fmt.Sprintf("user_%d_%d", payment.UserID, time.Now().Unix())
	addReq := &wgagent.AddPeerRequest{
		Interface:  "wg0",
		PublicKey:  peerResp.PublicKey,
		AllowedIP:  peerResp.AllowedIP,
		KeepaliveS: 25,
		PeerID:     peerID,
	}

	_, err = wgClient.AddPeer(ctx, addReq)
	if err != nil {
		return nil, fmt.Errorf("ошибка добавления пира: %w", err)
	}

	// Создаем подписку
	startDate := time.Now()
	endDate := startDate.AddDate(0, 0, payment.Plan.DurationDays)

	subscription := &db.Subscription{
		UserID:     payment.UserID,
		PlanID:     payment.PlanID,
		PeerID:     peerID,
		PrivKeyEnc: peerResp.PrivateKey,
		PublicKey:  peerResp.PublicKey,
		Interface:  "wg0",
		AllowedIP:  peerResp.AllowedIP,
		Platform:   "generic", // Платформа будет установлена позже
		StartDate:  startDate,
		EndDate:    endDate,
		Active:     true,
		PaymentID:  &payment.ID,
	}

	if err := tx.Create(subscription).Error; err != nil {
		return nil, fmt.Errorf("ошибка создания подписки в БД: %w", err)
	}

	return subscription, nil
}
