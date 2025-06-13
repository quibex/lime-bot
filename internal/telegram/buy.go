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

type BuyState struct {
	UserID   int64
	PlanID   uint
	Platform string
	Qty      int
	MethodID uint
	Step     string 
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

	
	var keyboard [][]tgbotapi.InlineKeyboardButton
	for _, plan := range plans {
		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%s - %d руб. (%d дней)", plan.Name, plan.PriceInt, plan.DurationDays),
			fmt.Sprintf("buy_plan_%d", plan.ID),
		)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
	}

	
	buyStates[msg.From.ID] = &BuyState{
		UserID: msg.From.ID,
		Step:   "plan",
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

	if strings.HasPrefix(data, "buy_plan_") {
		s.handlePlanSelection(callback, state)
	} else if strings.HasPrefix(data, "buy_platform_") {
		s.handlePlatformSelection(callback, state)
	} else if strings.HasPrefix(data, "buy_qty_") {
		s.handleQtySelection(callback, state)
	} else if strings.HasPrefix(data, "buy_method_") {
		s.handleMethodSelection(callback, state)
	}
}

func (s *Service) handlePlanSelection(callback *tgbotapi.CallbackQuery, state *BuyState) {
	planIDStr := strings.TrimPrefix(callback.Data, "buy_plan_")
	planID, err := strconv.ParseUint(planIDStr, 10, 32)
	if err != nil {
		s.answerCallback(callback.ID, "Неверный ID тарифа")
		return
	}

	state.PlanID = uint(planID)
	state.Step = "platform"

	
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("📱 Android", "buy_platform_android")},
		{tgbotapi.NewInlineKeyboardButtonData("🍎 iOS", "buy_platform_ios")},
		{tgbotapi.NewInlineKeyboardButtonData("🪟 Windows", "buy_platform_windows")},
		{tgbotapi.NewInlineKeyboardButtonData("🐧 Linux", "buy_platform_linux")},
		{tgbotapi.NewInlineKeyboardButtonData("🍏 macOS", "buy_platform_macos")},
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
	platform := strings.TrimPrefix(callback.Data, "buy_platform_")
	state.Platform = platform
	state.Step = "qty"

	
	keyboard := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("1 ключ", "buy_qty_1")},
		{tgbotapi.NewInlineKeyboardButtonData("2 ключа", "buy_qty_2")},
		{tgbotapi.NewInlineKeyboardButtonData("3 ключа", "buy_qty_3")},
		{tgbotapi.NewInlineKeyboardButtonData("5 ключей", "buy_qty_5")},
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
	qtyStr := strings.TrimPrefix(callback.Data, "buy_qty_")
	qty, err := strconv.Atoi(qtyStr)
	if err != nil {
		s.answerCallback(callback.ID, "Неверное количество")
		return
	}

	state.Qty = qty
	state.Step = "method"

	
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

	
	var keyboard [][]tgbotapi.InlineKeyboardButton
	for _, method := range methods {
		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%s (%s)", method.Bank, method.PhoneNumber),
			fmt.Sprintf("buy_method_%d", method.ID),
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
	methodIDStr := strings.TrimPrefix(callback.Data, "buy_method_")
	methodID, err := strconv.ParseUint(methodIDStr, 10, 32)
	if err != nil {
		s.answerCallback(callback.ID, "Неверный ID метода")
		return
	}

	state.MethodID = uint(methodID)

	
	err = s.processPurchase(callback, state)
	if err != nil {
		s.answerCallback(callback.ID, fmt.Sprintf("Ошибка обработки покупки: %v", err))
		return
	}

	
	delete(buyStates, state.UserID)
	s.answerCallback(callback.ID, "Покупка обработана!")
}

func (s *Service) processPurchase(callback *tgbotapi.CallbackQuery, state *BuyState) error {
	
	tx := s.repo.DB().Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	
	var plan db.Plan
	if err := tx.First(&plan, state.PlanID).Error; err != nil {
		tx.Rollback()
		return err
	}

	
	var method db.PaymentMethod
	if err := tx.First(&method, state.MethodID).Error; err != nil {
		tx.Rollback()
		return err
	}

	
	totalAmount := plan.PriceInt * state.Qty
	payment := &db.Payment{
		UserID:   state.UserID,
		MethodID: state.MethodID,
		Amount:   totalAmount,
		PlanID:   state.PlanID,
		Qty:      state.Qty,
		Status:   "pending",
	}

	if err := tx.Create(payment).Error; err != nil {
		tx.Rollback()
		return err
	}

	
	for i := 0; i < state.Qty; i++ {
		subscription, err := s.createSubscription(tx, state, &plan, payment.ID)
		if err != nil {
			tx.Rollback()
			return err
		}

		
		s.sendSubscriptionToUser(callback.Message.Chat.ID, subscription)
	}

	
	if err := tx.Commit().Error; err != nil {
		return err
	}

	
	s.sendPaymentInfo(callback.Message.Chat.ID, payment, &method, &plan)

	return nil
}

func (s *Service) createSubscription(tx *gorm.DB, state *BuyState, plan *db.Plan, paymentID uint) (*db.Subscription, error) {
	
	ctx := context.Background()

	wgConfig := wgagent.Config{
		Addr: s.cfg.WGAgentAddr,
	}
	wgClient, err := wgagent.NewClient(wgConfig)
	if err != nil {
		return nil, err
	}
	defer wgClient.Close()

	
	peerReq := &wgagent.GeneratePeerConfigRequest{
		Interface:      "wg0",
		ServerEndpoint: "vpn.example.com:51820",
		DNSServers:     "1.1.1.1, 1.0.0.1",
		AllowedIPs:     "0.0.0.0/0",
	}

	peerResp, err := wgClient.GeneratePeerConfig(ctx, peerReq)
	if err != nil {
		return nil, err
	}

	
	peerID := fmt.Sprintf("user_%d_%d", state.UserID, time.Now().Unix())
	addReq := &wgagent.AddPeerRequest{
		Interface:  "wg0",
		PublicKey:  peerResp.PublicKey,
		AllowedIP:  peerResp.AllowedIP,
		KeepaliveS: 25,
		PeerID:     peerID,
	}

	_, err = wgClient.AddPeer(ctx, addReq)
	if err != nil {
		return nil, err
	}

	
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
		Platform:   state.Platform,
		StartDate:  startDate,
		EndDate:    endDate,
		Active:     true,
		PaymentID:  &paymentID,
	}

	if err := tx.Create(subscription).Error; err != nil {
		return nil, err
	}

	return subscription, nil
}

func (s *Service) sendSubscriptionToUser(chatID int64, subscription *db.Subscription) {
	text := fmt.Sprintf(`🔑 Ваш VPN ключ готов!

📋 ID: %s
📱 Платформа: %s
📅 Действует до: %s

📄 Конфигурация в следующем сообщении...`,
		subscription.PeerID,
		subscription.Platform,
		subscription.EndDate.Format("02.01.2006"),
	)

	s.reply(chatID, text)

	
	s.reply(chatID, "Конфигурация: mock_config_data")
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
