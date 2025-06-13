package telegram

import (
	"fmt"
	"strings"

	"lime-bot/internal/db"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (s *Service) handleAddPaymentMethod(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) < 3 {
		s.reply(msg.Chat.ID, "Использование: /addpmethod <телефон> <банк> <имя_владельца>\nПример: /addpmethod +79991234567 Сбербанк \"Иван Иванов\"")
		return
	}

	phone := args[0]
	bank := args[1]
	ownerName := strings.Join(args[2:], " ")

	
	ownerName = strings.Trim(ownerName, "\"")

	method := &db.PaymentMethod{
		PhoneNumber: phone,
		Bank:        bank,
		OwnerName:   ownerName,
	}

	result := s.repo.DB().Create(method)
	if result.Error != nil {
		s.reply(msg.Chat.ID, "Ошибка создания способа оплаты")
		return
	}

	s.reply(msg.Chat.ID, fmt.Sprintf("✅ Способ оплаты добавлен:\n📱 %s\n🏦 %s\n👤 %s", phone, bank, ownerName))
}

func (s *Service) handleListPaymentMethods(msg *tgbotapi.Message) {
	var methods []db.PaymentMethod
	result := s.repo.DB().Where("archived = false").Find(&methods)
	if result.Error != nil {
		s.reply(msg.Chat.ID, "Ошибка получения способов оплаты")
		return
	}

	if len(methods) == 0 {
		s.reply(msg.Chat.ID, "Способы оплаты пока не добавлены")
		return
	}

	text := "💳 Доступные способы оплаты:\n\n"
	for i, method := range methods {
		text += fmt.Sprintf("%d. 📱 %s\n🏦 %s\n👤 %s\n\n",
			i+1, method.PhoneNumber, method.Bank, method.OwnerName)
	}

	s.reply(msg.Chat.ID, text)
}

func (s *Service) handleArchivePaymentMethod(msg *tgbotapi.Message) {
	var methods []db.PaymentMethod
	result := s.repo.DB().Where("archived = false").Find(&methods)
	if result.Error != nil {
		s.reply(msg.Chat.ID, "Ошибка получения способов оплаты")
		return
	}

	if len(methods) == 0 {
		s.reply(msg.Chat.ID, "Нет активных способов оплаты")
		return
	}

	
	var keyboard [][]tgbotapi.InlineKeyboardButton
	for _, method := range methods {
		btn := tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%s (%s)", method.Bank, method.PhoneNumber),
			fmt.Sprintf("archive_method_%d", method.ID),
		)
		keyboard = append(keyboard, []tgbotapi.InlineKeyboardButton{btn})
	}

	msgConfig := tgbotapi.NewMessage(msg.Chat.ID, "Выберите способ оплаты для архивирования:")
	msgConfig.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	s.bot.Send(msgConfig)
}
