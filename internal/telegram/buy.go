package telegram

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"lime-bot/internal/db"
	"lime-bot/internal/gates/wgagent"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gorm.io/gorm"
)

type BuyState struct {
	UserID    int64
	PlanID    uint
	Platform  Platform
	Qty       int
	MethodID  uint
	PaymentID uint
	Step      BuyStep
}

var buyStates = make(map[int64]*BuyState)

func (s *Service) handleBuy(msg *tgbotapi.Message) {

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

	// Создаем клавиатуру с тарифами
	var keyboard [][]tgbotapi.InlineKeyboardButton
	for _, plan := range plans {
		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%s - %d руб. (%d дней)", plan.Name, plan.PriceInt, plan.DurationDays),
			CallbackBuyPlan.WithID(plan.ID),
		)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
	}

	buyStates[msg.From.ID] = &BuyState{
		UserID: msg.From.ID,
		Step:   BuyStepPlan,
	}

	msgConfig := tgbotapi.NewMessage(msg.Chat.ID, "Выберите тариф:")
	msgConfig.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	s.bot.Send(msgConfig)
}

func (s *Service) handleBuyCallback(callback *tgbotapi.CallbackQuery) {
	data := callback.Data
	userID := callback.From.ID

	state, exists := buyStates[userID]
	if !exists {
		s.answerCallback(callback.ID, "Состояние покупки не найдено")
		return
	}

	if strings.HasPrefix(data, CallbackBuyPlan.String()) {
		s.handlePlanSelection(callback, state)
	} else if strings.HasPrefix(data, CallbackBuyPlatform.String()) {
		s.handlePlatformSelection(callback, state)
	} else if strings.HasPrefix(data, CallbackBuyQty.String()) {
		s.handleQtySelection(callback, state)
	} else if strings.HasPrefix(data, CallbackBuyMethod.String()) {
		s.handleMethodSelection(callback, state)
	}
}

func (s *Service) handlePlanSelection(callback *tgbotapi.CallbackQuery, state *BuyState) {
	planIDStr := strings.TrimPrefix(callback.Data, CallbackBuyPlan.String())
	planID, err := strconv.ParseUint(planIDStr, 10, 32)
	if err != nil {
		s.answerCallback(callback.ID, "Неверный ID тарифа")
		return
	}

	state.PlanID = uint(planID)
	state.Step = BuyStepPlatform

	// Выбор платформы
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData(
			PlatformAndroid.Emoji()+" "+PlatformAndroid.DisplayName(),
			CallbackBuyPlatform.WithID(PlatformAndroid.String()))},
		{tgbotapi.NewInlineKeyboardButtonData(
			PlatformIOS.Emoji()+" "+PlatformIOS.DisplayName(),
			CallbackBuyPlatform.WithID(PlatformIOS.String()))},
		{tgbotapi.NewInlineKeyboardButtonData(
			PlatformWindows.Emoji()+" "+PlatformWindows.DisplayName(),
			CallbackBuyPlatform.WithID(PlatformWindows.String()))},
		{tgbotapi.NewInlineKeyboardButtonData(
			PlatformLinux.Emoji()+" "+PlatformLinux.DisplayName(),
			CallbackBuyPlatform.WithID(PlatformLinux.String()))},
		{tgbotapi.NewInlineKeyboardButtonData(
			PlatformMacOS.Emoji()+" "+PlatformMacOS.DisplayName(),
			CallbackBuyPlatform.WithID(PlatformMacOS.String()))},
	}

	editMsg := tgbotapi.NewEditMessageText(
		callback.Message.Chat.ID,
		callback.Message.MessageID,
		"Выберите платформу:",
	)
	editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboard}
	s.bot.Send(editMsg)
	s.answerCallback(callback.ID, "")
}

func (s *Service) handlePlatformSelection(callback *tgbotapi.CallbackQuery, state *BuyState) {
	platformStr := strings.TrimPrefix(callback.Data, CallbackBuyPlatform.String())
	platform := Platform(platformStr)

	if !platform.IsValid() {
		s.answerCallback(callback.ID, "Неверная платформа")
		return
	}

	state.Platform = platform
	state.Step = BuyStepQty

	// Выбор количества ключей
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("1 ключ", CallbackBuyQty.WithID("1"))},
		{tgbotapi.NewInlineKeyboardButtonData("2 ключа", CallbackBuyQty.WithID("2"))},
		{tgbotapi.NewInlineKeyboardButtonData("3 ключа", CallbackBuyQty.WithID("3"))},
		{tgbotapi.NewInlineKeyboardButtonData("5 ключей", CallbackBuyQty.WithID("5"))},
	}

	editMsg := tgbotapi.NewEditMessageText(
		callback.Message.Chat.ID,
		callback.Message.MessageID,
		"Выберите количество ключей:",
	)
	editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboard}
	s.bot.Send(editMsg)
	s.answerCallback(callback.ID, "")
}

func (s *Service) handleQtySelection(callback *tgbotapi.CallbackQuery, state *BuyState) {
	qtyStr := strings.TrimPrefix(callback.Data, CallbackBuyQty.String())
	qty, err := strconv.Atoi(qtyStr)
	if err != nil {
		s.answerCallback(callback.ID, "Неверное количество")
		return
	}

	state.Qty = qty
	state.Step = BuyStepMethod

	// Получаем способы оплаты
	var methods []db.PaymentMethod
	result := s.repo.DB().Where("archived = false").Find(&methods)
	if result.Error != nil {
		s.answerCallback(callback.ID, "Ошибка получения способов оплаты")
		return
	}

	if len(methods) == 0 {
		s.answerCallback(callback.ID, "Способы оплаты не настроены")
		return
	}

	// Создаем клавиатуру с методами оплаты
	var keyboard [][]tgbotapi.InlineKeyboardButton
	for _, method := range methods {
		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%s (%s)", method.Bank, method.PhoneNumber),
			CallbackBuyMethod.WithID(method.ID),
		)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
	}

	editMsg := tgbotapi.NewEditMessageText(
		callback.Message.Chat.ID,
		callback.Message.MessageID,
		"Выберите способ оплаты:",
	)
	editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboard}
	s.bot.Send(editMsg)
	s.answerCallback(callback.ID, "")
}

func (s *Service) handleMethodSelection(callback *tgbotapi.CallbackQuery, state *BuyState) {
	methodIDStr := strings.TrimPrefix(callback.Data, CallbackBuyMethod.String())
	methodID, err := strconv.ParseUint(methodIDStr, 10, 32)
	if err != nil {
		s.answerCallback(callback.ID, "Неверный ID метода")
		return
	}

	state.MethodID = uint(methodID)

	// Переходим к оплате
	err = s.processPurchase(callback, state)
	if err != nil {
		s.answerCallback(callback.ID, fmt.Sprintf("Ошибка обработки покупки: %v", err))
		return
	}

	state.Step = BuyStepReceipt
	s.answerCallback(callback.ID, "Следуйте инструкциям по оплате")
}

func (s *Service) processPurchase(callback *tgbotapi.CallbackQuery, state *BuyState) error {
	slog.Info("Processing purchase", "user_id", state.UserID, "plan_id", state.PlanID, "qty", state.Qty)

	// Создаем транзакцию
	tx := s.repo.DB().Begin()
	if tx.Error != nil {
		err := ErrDatabasef("Failed to begin purchase transaction: %v", tx.Error)
		s.logAndReportError("Purchase transaction failed", err, map[string]interface{}{
			"user_id": state.UserID,
			"plan_id": state.PlanID,
		})
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			slog.Error("Purchase panic", "user_id", state.UserID, "panic", r)
		}
	}()

	// Получаем план
	var plan db.Plan
	if err := tx.First(&plan, state.PlanID).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			planErr := ErrPlanNotFoundf("Plan #%v not found", state.PlanID)
			s.logAndReportError("Plan not found during purchase", planErr, map[string]interface{}{
				"user_id": state.UserID,
				"plan_id": state.PlanID,
			})
			return planErr
		}
		dbErr := ErrDatabasef("Failed to fetch plan #%v: %v", state.PlanID, err)
		s.logAndReportError("Plan fetch failed", dbErr, map[string]interface{}{
			"user_id": state.UserID,
			"plan_id": state.PlanID,
		})
		return dbErr
	}

	slog.Info("Plan fetched for purchase", "plan_id", plan.ID, "plan_name", plan.Name, "price", plan.PriceInt)

	// Получаем способ оплаты
	var method db.PaymentMethod
	if err := tx.First(&method, state.MethodID).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			methodErr := ErrPaymentf("Payment method #%v not found", state.MethodID)
			s.logAndReportError("Payment method not found", methodErr, map[string]interface{}{
				"user_id":   state.UserID,
				"method_id": state.MethodID,
			})
			return methodErr
		}
		dbErr := ErrDatabasef("Failed to fetch payment method #%v: %v", state.MethodID, err)
		s.logAndReportError("Payment method fetch failed", dbErr, map[string]interface{}{
			"user_id":   state.UserID,
			"method_id": state.MethodID,
		})
		return dbErr
	}

	slog.Info("Payment method fetched", "method_id", method.ID, "bank", method.Bank)

	// Создаем запись о платеже
	totalAmount := plan.PriceInt * state.Qty
	payment := &db.Payment{
		UserID:   state.UserID,
		MethodID: state.MethodID,
		Amount:   totalAmount,
		PlanID:   state.PlanID,
		Qty:      state.Qty,
		Status:   PaymentStatusPending.String(),
	}

	slog.Info("Creating payment record", "amount", totalAmount, "qty", state.Qty, "user_id", state.UserID)

	if err := tx.Create(payment).Error; err != nil {
		tx.Rollback()
		paymentErr := ErrDatabasef("Failed to create payment record: %v", err)
		s.logAndReportError("Payment creation failed", paymentErr, map[string]interface{}{
			"user_id": state.UserID,
			"amount":  totalAmount,
			"plan_id": state.PlanID,
		})
		return paymentErr
	}

	// Сохраняем изменения
	if err := tx.Commit().Error; err != nil {
		commitErr := ErrDatabasef("Failed to commit purchase transaction: %v", err)
		s.logAndReportError("Purchase commit failed", commitErr, map[string]interface{}{
			"user_id":    state.UserID,
			"payment_id": payment.ID,
		})
		return commitErr
	}

	slog.Info("Payment created successfully", "payment_id", payment.ID, "user_id", state.UserID, "amount", totalAmount)

	state.PaymentID = payment.ID

	// Отправляем инструкции по оплате
	s.sendPaymentInstructions(callback.Message.Chat.ID, payment, &method, &plan)

	return nil
}

func (s *Service) createSubscription(tx *gorm.DB, state *BuyState, plan *db.Plan, paymentID uint) (*db.Subscription, string, string, error) {
	slog.Info("Creating subscription", "user_id", state.UserID, "plan_id", state.PlanID, "payment_id", paymentID)

	ctx := context.Background()

	wgConfig := wgagent.Config{
		Addr:     s.cfg.WGAgentAddr,
		CertFile: s.cfg.WGClientCert,
		KeyFile:  s.cfg.WGClientKey,
		CAFile:   s.cfg.WGCACert,
	}

	// Проверяем, есть ли сертификаты для secure соединения
	if s.cfg.WGClientCert == "" || s.cfg.WGClientKey == "" || s.cfg.WGCACert == "" {
		slog.Warn("WG certificates not configured, using insecure connection")
		wgConfig = wgagent.Config{
			Addr: s.cfg.WGAgentAddr,
		}
	}

	wgClient, err := wgagent.NewClient(wgConfig)
	if err != nil {
		wgErr := ErrWGAgentf("Failed to create WG client: %v", err)
		s.logAndReportError("WG client creation failed", wgErr, map[string]interface{}{
			"user_id":    state.UserID,
			"payment_id": paymentID,
			"wg_addr":    s.cfg.WGAgentAddr,
		})
		return nil, "", "", wgErr
	}
	defer wgClient.Close()

	slog.Info("WG client created successfully", "user_id", state.UserID)

	peerReq := &wgagent.GeneratePeerConfigRequest{
		Interface:      "wg0",
		ServerEndpoint: s.cfg.WGServerEndpoint,
		DNSServers:     "1.1.1.1, 1.0.0.1",
		AllowedIPs:     "0.0.0.0/0",
	}

	slog.Info("Generating peer config", "user_id", state.UserID, "server_endpoint", s.cfg.WGServerEndpoint)

	peerResp, err := wgClient.GeneratePeerConfig(ctx, peerReq)
	if err != nil {
		wgErr := ErrWGAgentf("Failed to generate peer config: %v", err)
		s.logAndReportError("Peer config generation failed", wgErr, map[string]interface{}{
			"user_id":    state.UserID,
			"payment_id": paymentID,
			"interface":  "wg0",
		})
		return nil, "", "", wgErr
	}

	slog.Info("Peer config generated", "user_id", state.UserID, "public_key", peerResp.PublicKey[:10]+"...")

	peerID := "user_" + strconv.FormatInt(state.UserID, 10) + "_" + strconv.FormatInt(time.Now().Unix(), 10)
	addReq := &wgagent.AddPeerRequest{
		Interface:  "wg0",
		PublicKey:  peerResp.PublicKey,
		AllowedIP:  peerResp.AllowedIP,
		KeepaliveS: 25,
		PeerID:     peerID,
	}

	slog.Info("Adding peer to interface", "user_id", state.UserID, "peer_id", peerID, "allowed_ip", peerResp.AllowedIP)

	addResp, err := wgClient.AddPeer(ctx, addReq)
	if err != nil {
		wgErr := ErrWGAgentf("Failed to add peer: %v", err)
		s.logAndReportError("Peer addition failed", wgErr, map[string]interface{}{
			"user_id":    state.UserID,
			"peer_id":    peerID,
			"public_key": peerResp.PublicKey,
		})
		return nil, "", "", wgErr
	}

	slog.Info("Peer added successfully", "user_id", state.UserID, "peer_id", peerID)

	startDate := time.Now()
	endDate := startDate.AddDate(0, 0, plan.DurationDays)

	subscription := &db.Subscription{
		UserID:     state.UserID,
		PlanID:     state.PlanID,
		PeerID:     peerID,
		PrivKeyEnc: peerResp.PrivateKey,
		PublicKey:  peerResp.PublicKey,
		Interface:  "wg0",
		AllowedIP:  peerResp.AllowedIP,
		Platform:   state.Platform.String(),
		StartDate:  startDate,
		EndDate:    endDate,
		Active:     true,
		PaymentID:  &paymentID,
	}

	slog.Info("Creating subscription in database",
		"user_id", state.UserID,
		"peer_id", peerID,
		"platform", state.Platform,
		"end_date", endDate.Format("2006-01-02"),
	)

	if err := tx.Create(subscription).Error; err != nil {
		dbErr := ErrDatabasef("Failed to create subscription: %v", err)
		s.logAndReportError("Subscription creation failed", dbErr, map[string]interface{}{
			"user_id":    state.UserID,
			"peer_id":    peerID,
			"payment_id": paymentID,
		})
		return nil, "", "", dbErr
	}

	slog.Info("Subscription created successfully", "subscription_id", subscription.ID, "user_id", state.UserID)
	cfg := addResp.Config
	qr := addResp.QRCode
	if cfg == "" {
		cfg = peerResp.Config
		qr = peerResp.QRCode
	}
	return subscription, cfg, qr, nil
}

func (s *Service) sendSubscriptionToUser(chatID int64, subscription *db.Subscription) {
	s.sendSubscriptionToUserWithData(chatID, subscription, "", "")
}

func (s *Service) sendSubscriptionToUserWithData(chatID int64, subscription *db.Subscription, config string, qr string) {
	text := fmt.Sprintf(`🔑 Ваш VPN ключ готов!

📋 ID: %s
📱 Платформа: %s
📅 Действует до: %s`,
		subscription.PeerID,
		subscription.Platform,
		subscription.EndDate.Format("02.01.2006"),
	)

	s.reply(chatID, text)

	if Platform(subscription.Platform) == PlatformAndroid || Platform(subscription.Platform) == PlatformIOS {
		if qr != "" {
			data, err := base64.StdEncoding.DecodeString(qr)
			if err == nil {
				photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{Name: "qr.png", Bytes: data})
				photo.Caption = "Отсканируйте QR код"
				s.bot.Send(photo)
				return
			}
		}
	}

	if config == "" {
		config = s.generateWireguardConfig(subscription)
	}

	file := tgbotapi.FileBytes{Name: "config.conf", Bytes: []byte(config)}
	doc := tgbotapi.NewDocument(chatID, file)
	doc.Caption = "Конфигурация WireGuard"
	s.bot.Send(doc)
}

func (s *Service) sendPaymentInfo(chatID int64, payment *db.Payment, method *db.PaymentMethod, plan *db.Plan) {
	text := fmt.Sprintf(`💳 Информация о платеже:

💰 Сумма: %d руб.
📦 Тариф: %s
🔢 Количество: %d
📱 Способ оплаты: %s (%s)

👤 Получатель: %s
📞 Телефон: %s

Статус: ⏳ Ожидает подтверждения`,
		payment.Amount,
		plan.Name,
		payment.Qty,
		method.Bank,
		method.PhoneNumber,
		method.OwnerName,
		method.PhoneNumber,
	)

	s.reply(chatID, text)
}

func (s *Service) sendPaymentInstructions(chatID int64, payment *db.Payment, method *db.PaymentMethod, plan *db.Plan) {
	text := fmt.Sprintf(`💳 Инструкции по оплате:

💰 Сумма: %d руб.
📦 Тариф: %s
🔢 Количество: %d

💳 Реквизиты для оплаты:
🏦 Банк: %s
📞 Номер карты/телефона: %s
👤 Получатель: %s

📋 Номер заказа: #%d

⚠️ ВАЖНО:
1. Переведите точную сумму: %d руб.
2. После оплаты отправьте скриншот или PDF чек
3. Укажите номер заказа #%d в сообщении
4. Ключи будут выданы после проверки платежа

⏰ Ожидайте подтверждения от администратора`,
		payment.Amount,
		plan.Name,
		payment.Qty,
		method.Bank,
		method.PhoneNumber,
		method.OwnerName,
		payment.ID,
		payment.Amount,
		payment.ID,
	)

	s.reply(chatID, text)
}

func (s *Service) handleReceiptMessage(msg *tgbotapi.Message) {
	// Проверить только наличие pending платежа, а не состояние buyStates
	var payment db.Payment
	result := s.repo.DB().Where("user_id = ? AND status = ?", msg.From.ID, PaymentStatusPending).
		Preload("Plan").Preload("User").First(&payment)

	if result.Error != nil {
		return // Нет pending платежа
	}

	// Получить FileID чека
	var fileID string
	if msg.Photo != nil && len(msg.Photo) > 0 {
		fileID = msg.Photo[len(msg.Photo)-1].FileID
	} else if msg.Document != nil {
		fileID = msg.Document.FileID
	} else {
		return
	}

	// Начинаем транзакцию
	tx := s.repo.DB().Begin()
	if tx.Error != nil {
		s.reply(msg.Chat.ID, "Ошибка БД")
		return
	}

	// Сохранить чек в БД
	payment.ReceiptFileID = fileID
	if err := tx.Save(&payment).Error; err != nil {
		tx.Rollback()
		s.logAndReportError("Failed to save receipt", err, map[string]interface{}{
			"payment_id": payment.ID,
			"user_id":    msg.From.ID,
		})
		s.reply(msg.Chat.ID, "Ошибка сохранения чека")
		return
	}

	// СРАЗУ создаем подписки и выдаем ключи
	slog.Info("Creating subscriptions immediately after receipt", "payment_id", payment.ID, "qty", payment.Qty)

	for i := 0; i < payment.Qty; i++ {
		subscription, err := s.createSubscriptionForPayment(tx, &payment)
		if err != nil {
			tx.Rollback()
			s.handleError(msg.Chat.ID, err)
			return
		}
		// Отправляем ключи пользователю
		s.sendSubscriptionToUser(msg.Chat.ID, subscription)
	}

	if err := tx.Commit().Error; err != nil {
		s.reply(msg.Chat.ID, "Ошибка БД")
		return
	}

	s.reply(msg.Chat.ID, "✅ Чек получен! Ваши ключи выше. Ожидайте подтверждения кассира.")

	// Уведомить кассиров о новом чеке для проверки
	s.notifyCashiersAboutReceipt(&payment)
}

func (s *Service) notifyCashiersAboutReceipt(payment *db.Payment) {
	slog.Info("Notifying cashiers about new receipt", "payment_id", payment.ID, "user_id", payment.UserID)

	// Найти всех кассиров
	var cashiers []db.Admin
	s.repo.DB().Where("role = ? AND disabled = false", RoleCashier.String()).Find(&cashiers)

	// Если нет кассиров, уведомить всех админов
	if len(cashiers) == 0 {
		slog.Info("No cashiers found, notifying all admins", "payment_id", payment.ID)
		s.repo.DB().Where("role IN (?, ?) AND disabled = false", RoleAdmin.String(), RoleSuper.String()).Find(&cashiers)
	}

	if len(cashiers) == 0 {
		slog.Warn("No admins found to notify about receipt", "payment_id", payment.ID)
		return
	}

	text := fmt.Sprintf(`💳 Новый чек для проверки!

📋 Заказ #%d  
👤 Пользователь: @%s
💰 Сумма: %d руб.
📦 Тариф: %s

Проверьте в /payqueue`, payment.ID, payment.User.Username, payment.Amount, payment.Plan.Name)

	for _, cashier := range cashiers {
		slog.Info("Notifying cashier about receipt", "payment_id", payment.ID, "cashier_id", cashier.TgID)
		s.reply(cashier.TgID, text)
	}
}

func (s *Service) generateWireguardConfig(subscription *db.Subscription) string {
	config := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/32
DNS = 1.1.1.1, 1.0.0.1

[Peer]
PublicKey = SERVER_PUBLIC_KEY_HERE
Endpoint = %s
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25`,
		subscription.PrivKeyEnc,
		subscription.AllowedIP,
		s.cfg.WGServerEndpoint,
	)

	return config
}
