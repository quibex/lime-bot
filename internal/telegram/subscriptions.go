package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"lime-bot/internal/db"
	"lime-bot/internal/gates/wgagent"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gorm.io/gorm"
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
	slog.Info("Disable subscription requested", "admin_id", msg.From.ID, "username", msg.From.UserName)

	if !s.isAdmin(msg.From.ID) {
		s.logAndReportError("Disable access denied", ErrPermission("User attempted disable without admin rights"), map[string]interface{}{
			"user_id":  msg.From.ID,
			"username": msg.From.UserName,
		})
		s.reply(msg.Chat.ID, "У вас нет прав для этой команды")
		return
	}

	args := strings.Fields(msg.CommandArguments())
	if len(args) == 0 {
		s.reply(msg.Chat.ID, "Использование: /disable <username>\nПример: /disable john_doe")
		return
	}

	username := args[0]
	slog.Info("Processing disable request", "target_username", username, "admin_id", msg.From.ID)

	var user db.User
	result := s.repo.DB().Where("username = ?", username).First(&user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			userErr := ErrUserNotFoundf("User %v not found", username)
			s.logAndReportError("User not found for disable", userErr, map[string]interface{}{
				"target_username": username,
				"admin_id":        msg.From.ID,
			})
			s.reply(msg.Chat.ID, "Пользователь не найден: @"+username)
			return
		}

		dbErr := ErrDatabasef("Failed to find user %v: %v", username, result.Error)
		s.logAndReportError("Database error during user lookup", dbErr, map[string]interface{}{
			"target_username": username,
			"admin_id":        msg.From.ID,
		})
		s.reply(msg.Chat.ID, "Ошибка поиска пользователя")
		return
	}

	var subscriptions []db.Subscription
	result = s.repo.DB().Where("user_id = ? AND active = true", user.TgID).Find(&subscriptions)
	if result.Error != nil {
		dbErr := ErrDatabasef("Failed to fetch user subscriptions: %v", result.Error)
		s.logAndReportError("Failed to fetch subscriptions for disable", dbErr, map[string]interface{}{
			"target_user_id": user.TgID,
			"admin_id":       msg.From.ID,
		})
		s.reply(msg.Chat.ID, "Ошибка получения подписок пользователя")
		return
	}

	if len(subscriptions) == 0 {
		s.reply(msg.Chat.ID, "У пользователя @"+username+" нет активных подписок")
		return
	}

	slog.Info("Disabling user subscriptions", "target_user_id", user.TgID, "subscriptions_count", len(subscriptions))

	disabled := 0
	for _, sub := range subscriptions {
		slog.Info("Disabling subscription", "subscription_id", sub.ID, "peer_id", sub.PeerID)

		if err := s.disablePeer(sub.Interface, sub.PublicKey); err != nil {
			slog.Error("Failed to disable peer", "subscription_id", sub.ID, "error", err)
			continue
		}

		if err := s.repo.DB().Model(&sub).Update("active", false).Error; err != nil {
			dbErr := ErrDatabasef("Failed to update subscription status: %v", err)
			s.logAndReportError("Subscription status update failed", dbErr, map[string]interface{}{
				"subscription_id": sub.ID,
				"admin_id":        msg.From.ID,
			})
			continue
		}

		disabled++
	}

	slog.Info("User subscriptions disabled", "target_user_id", user.TgID, "disabled_count", disabled, "admin_id", msg.From.ID)
	s.reply(msg.Chat.ID, "✅ Отключено "+strconv.Itoa(disabled)+" подписок для @"+username)
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
	slog.Info("Disabling peer", "interface", interfaceName, "public_key", publicKey[:10]+"...")

	ctx := context.Background()

	wgConfig := wgagent.Config{
		Addr:     s.cfg.WGAgentAddr,
		CertFile: s.cfg.WGClientCert,
		KeyFile:  s.cfg.WGClientKey,
		CAFile:   s.cfg.WGCACert,
	}

	if s.cfg.WGClientCert == "" || s.cfg.WGClientKey == "" || s.cfg.WGCACert == "" {
		slog.Warn("WG certificates not configured for disable operation")
		wgConfig = wgagent.Config{
			Addr: s.cfg.WGAgentAddr,
		}
	}

	wgClient, err := wgagent.NewClient(wgConfig)
	if err != nil {
		wgErr := ErrWGAgentf("Failed to create WG client for disable: %v", err)
		s.logAndReportError("WG client creation failed for disable", wgErr, map[string]interface{}{
			"interface":  interfaceName,
			"public_key": publicKey,
			"wg_addr":    s.cfg.WGAgentAddr,
		})
		return wgErr
	}
	defer wgClient.Close()

	req := &wgagent.DisablePeerRequest{
		Interface: interfaceName,
		PublicKey: publicKey,
	}

	err = wgClient.DisablePeer(ctx, req)
	if err != nil {
		wgErr := ErrWGAgentf("Failed to disable peer: %v", err)
		s.logAndReportError("Peer disable operation failed", wgErr, map[string]interface{}{
			"interface":  interfaceName,
			"public_key": publicKey,
		})
		return wgErr
	}

	slog.Info("Peer disabled successfully", "interface", interfaceName, "public_key", publicKey[:10]+"...")
	return nil
}

func (s *Service) enablePeer(interfaceName, publicKey string) error {
	slog.Info("Enabling peer", "interface", interfaceName, "public_key", publicKey[:10]+"...")

	ctx := context.Background()

	wgConfig := wgagent.Config{
		Addr:     s.cfg.WGAgentAddr,
		CertFile: s.cfg.WGClientCert,
		KeyFile:  s.cfg.WGClientKey,
		CAFile:   s.cfg.WGCACert,
	}

	if s.cfg.WGClientCert == "" || s.cfg.WGClientKey == "" || s.cfg.WGCACert == "" {
		slog.Warn("WG certificates not configured for enable operation")
		wgConfig = wgagent.Config{
			Addr: s.cfg.WGAgentAddr,
		}
	}

	wgClient, err := wgagent.NewClient(wgConfig)
	if err != nil {
		wgErr := ErrWGAgentf("Failed to create WG client for enable: %v", err)
		s.logAndReportError("WG client creation failed for enable", wgErr, map[string]interface{}{
			"interface":  interfaceName,
			"public_key": publicKey,
			"wg_addr":    s.cfg.WGAgentAddr,
		})
		return wgErr
	}
	defer wgClient.Close()

	req := &wgagent.EnablePeerRequest{
		Interface: interfaceName,
		PublicKey: publicKey,
	}

	err = wgClient.EnablePeer(ctx, req)
	if err != nil {
		wgErr := ErrWGAgentf("Failed to enable peer: %v", err)
		s.logAndReportError("Peer enable operation failed", wgErr, map[string]interface{}{
			"interface":  interfaceName,
			"public_key": publicKey,
		})
		return wgErr
	}

	slog.Info("Peer enabled successfully", "interface", interfaceName, "public_key", publicKey[:10]+"...")
	return nil
}
