package telegram

import (
	"context"
	"fmt"
	"strings"

	"lime-bot/internal/db"
	"lime-bot/internal/gates/wgagent"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (s *Service) handleMyKeys(msg *tgbotapi.Message) {
	var subscriptions []db.Subscription
	result := s.repo.DB().Where("user_id = ? AND active = true", msg.From.ID).
		Preload("Plan").Find(&subscriptions)

	if result.Error != nil {
		s.reply(msg.Chat.ID, "Ошибка получения подписок")
		return
	}

	if len(subscriptions) == 0 {
		s.reply(msg.Chat.ID, "У вас пока нет активных подписок. Используйте /buy для покупки.")
		return
	}

	text := "🔑 Ваши активные подписки:\n\n"
	for i, sub := range subscriptions {
		status := "🟢 Активен"
		if !sub.Active {
			status = "🔴 Отключен"
		}

		text += fmt.Sprintf("📱 %d. %s (%s)\n📋 ID: %s\n⏰ До: %s\n%s\n\n",
			i+1, sub.Plan.Name, sub.Platform, sub.PeerID,
			sub.EndDate.Format("02.01.2006"), status)
	}

	var keyboard [][]tgbotapi.InlineKeyboardButton
	for _, sub := range subscriptions {
		buttonRow := []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("📄 Config %s", sub.Platform),
				fmt.Sprintf("sub_config_%s", sub.PeerID),
			),
			tgbotapi.NewInlineKeyboardButtonData(
				"📷 QR",
				fmt.Sprintf("sub_qr_%s", sub.PeerID),
			),
		}
		keyboard = append(keyboard, buttonRow)
	}

	msgConfig := tgbotapi.NewMessage(msg.Chat.ID, text)
	if len(keyboard) > 0 {
		msgConfig.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	}
	s.bot.Send(msgConfig)
}

func (s *Service) handleDisable(msg *tgbotapi.Message) {
	if !s.isAdmin(msg.From.ID) {
		s.reply(msg.Chat.ID, "У вас нет прав для этой команды")
		return
	}

	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		s.reply(msg.Chat.ID, "Использование: /disable <username>\nПример: /disable john_doe")
		return
	}

	username := args[0]

	var user db.User
	result := s.repo.DB().Where("username LIKE ?", "%"+username+"%").First(&user)
	if result.Error != nil {
		s.reply(msg.Chat.ID, "Пользователь не найден")
		return
	}

	var subscriptions []db.Subscription
	result = s.repo.DB().Where("user_id = ? AND active = true", user.TgID).Find(&subscriptions)
	if result.Error != nil {
		s.reply(msg.Chat.ID, "Ошибка получения подписок")
		return
	}

	if len(subscriptions) == 0 {
		s.reply(msg.Chat.ID, "У пользователя нет активных подписок")
		return
	}

	disabled := 0
	for _, sub := range subscriptions {
		err := s.disablePeer(sub.Interface, sub.PublicKey)
		if err != nil {
			continue
		}

		s.repo.DB().Model(&sub).Update("active", false)
		disabled++
	}

	s.reply(msg.Chat.ID, fmt.Sprintf("✅ Отключено %d подписок пользователя %s", disabled, username))
}

func (s *Service) handleEnable(msg *tgbotapi.Message) {
	if !s.isAdmin(msg.From.ID) {
		s.reply(msg.Chat.ID, "У вас нет прав для этой команды")
		return
	}

	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		s.reply(msg.Chat.ID, "Использование: /enable <username>\nПример: /enable john_doe")
		return
	}

	username := args[0]

	var user db.User
	result := s.repo.DB().Where("username LIKE ?", "%"+username+"%").First(&user)
	if result.Error != nil {
		s.reply(msg.Chat.ID, "Пользователь не найден")
		return
	}

	var subscriptions []db.Subscription
	result = s.repo.DB().Where("user_id = ? AND active = false AND end_date > NOW()", user.TgID).Find(&subscriptions)
	if result.Error != nil {
		s.reply(msg.Chat.ID, "Ошибка получения подписок")
		return
	}

	if len(subscriptions) == 0 {
		s.reply(msg.Chat.ID, "У пользователя нет отключенных подписок")
		return
	}

	enabled := 0
	for _, sub := range subscriptions {
		err := s.enablePeer(sub.Interface, sub.PublicKey)
		if err != nil {
			continue
		}

		s.repo.DB().Model(&sub).Update("active", true)
		enabled++
	}

	s.reply(msg.Chat.ID, fmt.Sprintf("✅ Включено %d подписок пользователя %s", enabled, username))
}

func (s *Service) handleSubscriptionCallback(callback *tgbotapi.CallbackQuery) {
	data := callback.Data

	if strings.HasPrefix(data, "sub_config_") {
		peerID := strings.TrimPrefix(data, "sub_config_")
		s.sendConfigForPeer(callback, peerID)
		return
	}

	if strings.HasPrefix(data, "sub_qr_") {
		peerID := strings.TrimPrefix(data, "sub_qr_")
		s.sendQRForPeer(callback, peerID)
		return
	}
}

func (s *Service) sendConfigForPeer(callback *tgbotapi.CallbackQuery, peerID string) {

	var subscription db.Subscription
	result := s.repo.DB().Where("peer_id = ? AND user_id = ?", peerID, callback.From.ID).First(&subscription)
	if result.Error != nil {
		s.answerCallback(callback.ID, "Подписка не найдена")
		return
	}

	config := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s
DNS = 1.1.1.1, 1.0.0.1

[Peer]
PublicKey = server_public_key
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25`, subscription.PrivKeyEnc, subscription.AllowedIP)

	configBytes := []byte(config)
	fileName := fmt.Sprintf("%s.conf", subscription.Platform)

	fileBytes := tgbotapi.FileBytes{
		Name:  fileName,
		Bytes: configBytes,
	}

	document := tgbotapi.NewDocument(callback.Message.Chat.ID, fileBytes)
	document.Caption = fmt.Sprintf("🔑 Конфигурация для %s", subscription.Platform)

	s.bot.Send(document)
	s.answerCallback(callback.ID, "Конфигурация отправлена")
}

func (s *Service) sendQRForPeer(callback *tgbotapi.CallbackQuery, peerID string) {

	var subscription db.Subscription
	result := s.repo.DB().Where("peer_id = ? AND user_id = ?", peerID, callback.From.ID).First(&subscription)
	if result.Error != nil {
		s.answerCallback(callback.ID, "Подписка не найдена")
		return
	}

	s.reply(callback.Message.Chat.ID, "📷 QR код пока не реализован")
	s.answerCallback(callback.ID, "")
}

func (s *Service) disablePeer(interfaceName, publicKey string) error {
	ctx := context.Background()

	wgConfig := wgagent.Config{
		Addr:     s.cfg.WGAgentAddr,
		CertFile: s.cfg.WGClientCert,
		KeyFile:  s.cfg.WGClientKey,
		CAFile:   s.cfg.WGCACert,
	}
	wgClient, err := wgagent.NewClient(wgConfig)
	if err != nil {
		return fmt.Errorf("ошибка создания WG клиента: %w", err)
	}
	defer wgClient.Close()

	req := &wgagent.DisablePeerRequest{
		Interface: interfaceName,
		PublicKey: publicKey,
	}

	return wgClient.DisablePeer(ctx, req)
}

func (s *Service) enablePeer(interfaceName, publicKey string) error {
	ctx := context.Background()

	wgConfig := wgagent.Config{
		Addr:     s.cfg.WGAgentAddr,
		CertFile: s.cfg.WGClientCert,
		KeyFile:  s.cfg.WGClientKey,
		CAFile:   s.cfg.WGCACert,
	}
	wgClient, err := wgagent.NewClient(wgConfig)
	if err != nil {
		return fmt.Errorf("ошибка создания WG клиента: %w", err)
	}
	defer wgClient.Close()

	req := &wgagent.EnablePeerRequest{
		Interface: interfaceName,
		PublicKey: publicKey,
	}

	return wgClient.EnablePeer(ctx, req)
}
