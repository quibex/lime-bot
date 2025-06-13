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
	// Получаем или создаем реферальный код пользователя
	var user db.User
	result := s.repo.DB().First(&user, "tg_id = ?", msg.From.ID)
	if result.Error != nil {
		s.reply(msg.Chat.ID, "Ошибка получения профиля")
		return
	}

	// Если нет реферального кода, создаем новый
	if user.RefCode == "" {
		refCode := generateRefCode(user.TgID)
		user.RefCode = refCode
		s.repo.DB().Save(&user)
	}

	// Получаем статистику рефералов
	var referralCount int64
	s.repo.DB().Model(&db.Referral{}).Where("inviter_id = ?", user.TgID).Count(&referralCount)

	// Получаем имя бота
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
	// Проверяем, что есть канал для отзывов
	if s.cfg.ReviewsChannelID == "" {
		s.reply(msg.Chat.ID, "Канал отзывов не настроен")
		return
	}

	// Просим пользователя отправить отзыв
	text := `📝 Отправьте ваш отзыв о сервисе:

Вы можете отправить:
• Текстовое сообщение
• Фото с подписью
• Документ

Ваш отзыв будет анонимно переслан в канал отзывов.`

	// Сохраняем состояние ожидания отзыва
	feedbackStates[msg.From.ID] = true

	s.reply(msg.Chat.ID, text)
}

// Состояние ожидания отзыва
var feedbackStates = make(map[int64]bool)

func (s *Service) handleFeedbackMessage(msg *tgbotapi.Message) {
	// Проверяем, ожидается ли отзыв от пользователя
	if !feedbackStates[msg.From.ID] {
		return
	}

	// Убираем состояние ожидания
	delete(feedbackStates, msg.From.ID)

	// Парсим ID канала
	channelID, err := strconv.ParseInt(s.cfg.ReviewsChannelID, 10, 64)
	if err != nil {
		s.reply(msg.Chat.ID, "Ошибка настройки канала отзывов")
		return
	}

	// Получаем информацию о пользователе
	var user db.User
	s.repo.DB().First(&user, "tg_id = ?", msg.From.ID)

	// Формируем заголовок отзыва
	reviewHeader := fmt.Sprintf("📝 Новый отзыв\n👤 Пользователь: @%s\n\n", user.Username)

	// Отправляем отзыв в канал
	if msg.Text != "" {
		// Текстовый отзыв
		reviewText := reviewHeader + msg.Text
		reviewMsg := tgbotapi.NewMessage(channelID, reviewText)
		s.bot.Send(reviewMsg)

	} else if msg.Photo != nil {
		// Фото с подписью
		photo := msg.Photo[len(msg.Photo)-1] // Берем фото наивысшего качества
		caption := reviewHeader
		if msg.Caption != "" {
			caption += msg.Caption
		}

		photoMsg := tgbotapi.NewPhoto(channelID, tgbotapi.FileID(photo.FileID))
		photoMsg.Caption = caption
		s.bot.Send(photoMsg)

	} else if msg.Document != nil {
		// Документ
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
	// Извлекаем реферальный код из аргументов команды /start
	args := msg.CommandArguments()
	if !startsWith(args, "ref_") {
		// Обычный старт
		s.handleStart(msg)
		return
	}

	refCode := args[4:] // Убираем "ref_"

	// Ищем пользователя по реферальному коду
	var inviter db.User
	result := s.repo.DB().Where("ref_code = ?", refCode).First(&inviter)
	if result.Error != nil {
		// Реферальный код не найден, обычный старт
		s.handleStart(msg)
		return
	}

	// Проверяем, не приглашал ли пользователь сам себя
	if inviter.TgID == msg.From.ID {
		s.handleStart(msg)
		return
	}

	// Регистрируем пользователя
	user := &db.User{
		TgID:     msg.From.ID,
		Username: msg.From.UserName,
	}
	s.repo.DB().FirstOrCreate(user, "tg_id = ?", msg.From.ID)

	// Проверяем, не был ли уже создан этот реферал
	var existingReferral db.Referral
	result = s.repo.DB().Where("inviter_id = ? AND invitee_id = ?", inviter.TgID, user.TgID).First(&existingReferral)
	if result.Error == nil {
		// Реферал уже существует
		s.handleStart(msg)
		return
	}

	// Создаем запись о реферале
	referral := &db.Referral{
		InviterID: inviter.TgID,
		InviteeID: user.TgID,
	}
	s.repo.DB().Create(referral)

	// Отправляем приветствие с упоминанием пригласившего
	text := fmt.Sprintf(`Добро пожаловать в Lime VPN! 🍋

Вы перешли по реферальной ссылке от @%s

Доступные команды:
/plans - посмотреть тарифы
/help - справка`, inviter.Username)

	s.reply(msg.Chat.ID, text)

	// Уведомляем пригласившего
	notifyText := fmt.Sprintf("🎉 По вашей реферальной ссылке зарегистрировался @%s!", user.Username)
	s.reply(inviter.TgID, notifyText)
}

func generateRefCode(userID int64) string {
	// Генерируем случайный код на основе ID пользователя и случайных байт
	bytes := make([]byte, 4)
	rand.Read(bytes)

	// Комбинируем ID пользователя с случайными байтами
	code := fmt.Sprintf("%x%s", userID, hex.EncodeToString(bytes))

	// Ограничиваем длину
	if len(code) > 12 {
		code = code[:12]
	}

	return code
}

// Вспомогательная функция для проверки префикса
func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
