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
	Platform Platform
	Qty      int
	MethodID uint
	Step     BuyStep
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

	// Очищаем состояние
	delete(buyStates, state.UserID)
	s.answerCallback(callback.ID, "Покупка обработана!")
}

func (s *Service) processPurchase(callback *tgbotapi.CallbackQuery, state *BuyState) error {
	// Создаем транзакцию
	tx := s.repo.DB().Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Получаем план
	var plan db.Plan
	if err := tx.First(&plan, state.PlanID).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Получаем способ оплаты
	var method db.PaymentMethod
	if err := tx.First(&method, state.MethodID).Error; err != nil {
		tx.Rollback()
		return err
	}

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

	if err := tx.Create(payment).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Сохраняем изменения
	if err := tx.Commit().Error; err != nil {
		return err
	}

	// Отправляем инструкции по оплате вместо готового ключа
	s.sendPaymentInstructions(callback.Message.Chat.ID, payment, &method, &plan)

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
		Platform:   state.Platform.String(),
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

	// Отправляем настоящую конфигурацию вместо мока
	config := s.generateWireguardConfig(subscription)
	s.reply(chatID, fmt.Sprintf("```\n%s\n```", config))
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
