package telegram

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"

	"lime-bot/internal/db"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (s *Service) handleRef(msg *tgbotapi.Message) {

	var user db.User
	result := s.repo.DB().First(&user, "tg_id = ?", msg.From.ID)
	if result.Error != nil {
		s.reply(msg.Chat.ID, "Ошибка получения профиля")
		return
	}

	if user.RefCode == "" {
		refCode := generateRefCode(user.TgID)
		user.RefCode = refCode
		s.repo.DB().Save(&user)
	}

	var referralCount int64
	s.repo.DB().Model(&db.Referral{}).Where("inviter_id = ?", user.TgID).Count(&referralCount)

	botUsername := s.bot.Self.UserName

	text := fmt.Sprintf(`🔗 Ваша реферальная ссылка:

https://t.me/%s?start=ref_%s

📊 Статистика:
👥 Приглашено: %d человек

💰 Получайте бонусы за каждого приглашенного друга!`,
		botUsername,
		user.RefCode,
		referralCount,
	)

	s.reply(msg.Chat.ID, text)
}

func (s *Service) handleFeedback(msg *tgbotapi.Message) {

	if s.cfg.ReviewsChannelID == "" {
		s.reply(msg.Chat.ID, "Канал отзывов не настроен")
		return
	}

	text := `📝 Отправьте ваш отзыв о сервисе:

Вы можете отправить:
• Текстовое сообщение
• Фото с подписью
• Документ

Ваш отзыв будет анонимно переслан в канал отзывов.`

	feedbackStates[msg.From.ID] = true

	s.reply(msg.Chat.ID, text)
}

var feedbackStates = make(map[int64]bool)

func (s *Service) handleFeedbackMessage(msg *tgbotapi.Message) {

	if !feedbackStates[msg.From.ID] {
		return
	}

	delete(feedbackStates, msg.From.ID)

	channelID, err := strconv.ParseInt(s.cfg.ReviewsChannelID, 10, 64)
	if err != nil {
		s.reply(msg.Chat.ID, "Ошибка настройки канала отзывов")
		return
	}

	var user db.User
	s.repo.DB().First(&user, "tg_id = ?", msg.From.ID)

	reviewHeader := fmt.Sprintf("📝 Новый отзыв\n👤 Пользователь: @%s\n\n", user.Username)

	if msg.Text != "" {

		reviewText := reviewHeader + msg.Text
		reviewMsg := tgbotapi.NewMessage(channelID, reviewText)
		s.bot.Send(reviewMsg)

	} else if msg.Photo != nil {

		photo := msg.Photo[len(msg.Photo)-1]
		caption := reviewHeader
		if msg.Caption != "" {
			caption += msg.Caption
		}

		photoMsg := tgbotapi.NewPhoto(channelID, tgbotapi.FileID(photo.FileID))
		photoMsg.Caption = caption
		s.bot.Send(photoMsg)

	} else if msg.Document != nil {

		caption := reviewHeader
		if msg.Caption != "" {
			caption += msg.Caption
		}

		docMsg := tgbotapi.NewDocument(channelID, tgbotapi.FileID(msg.Document.FileID))
		docMsg.Caption = caption
		s.bot.Send(docMsg)
	}

	s.reply(msg.Chat.ID, "✅ Спасибо за отзыв! Он отправлен в канал отзывов.")
}

func (s *Service) handleStartWithRef(msg *tgbotapi.Message) {
	// Всегда создаем или обновляем пользователя в БД
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

	args := msg.CommandArguments()
	if !startsWith(args, "ref_") {
		s.showMainMenu(msg.Chat.ID, msg.From.ID)
		return
	}

	refCode := args[4:]

	var inviter db.User
	result := s.repo.DB().Where("ref_code = ?", refCode).First(&inviter)
	if result.Error != nil {
		s.showMainMenu(msg.Chat.ID, msg.From.ID)
		return
	}

	if inviter.TgID == msg.From.ID {
		s.showMainMenu(msg.Chat.ID, msg.From.ID)
		return
	}

	var existingReferral db.Referral
	result = s.repo.DB().Where("inviter_id = ? AND invitee_id = ?", inviter.TgID, user.TgID).First(&existingReferral)
	if result.Error == nil {
		s.showMainMenu(msg.Chat.ID, msg.From.ID)
		return
	}

	referral := &db.Referral{
		InviterID: inviter.TgID,
		InviteeID: user.TgID,
	}
	s.repo.DB().Create(referral)

	// Отправляем приветственное сообщение с информацией о реферале
	welcomeText := fmt.Sprintf("Добро пожаловать в Lime VPN! 🍋\n\nВы перешли по реферальной ссылке от @%s", inviter.Username)
	s.reply(msg.Chat.ID, welcomeText)

	// Показываем главное меню
	s.showMainMenu(msg.Chat.ID, msg.From.ID)

	// Уведомляем пригласившего
	notifyText := fmt.Sprintf("🎉 По вашей реферальной ссылке зарегистрировался @%s!", user.Username)
	s.reply(inviter.TgID, notifyText)
}

func generateRefCode(userID int64) string {

	bytes := make([]byte, 4)
	rand.Read(bytes)

	code := fmt.Sprintf("%x%s", userID, hex.EncodeToString(bytes))

	if len(code) > 12 {
		code = code[:12]
	}

	return code
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
