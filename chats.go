package tgbot

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (s *Service) GetChat(chatID int64) (*models.ChatFullInfo, error) {
	return s.bot.GetChat(context.Background(), &bot.GetChatParams{
		ChatID: chatID,
	})
}

func (s *Service) GetChatMember(chat, user int64) (*models.ChatMember, error) {
	return s.bot.GetChatMember(context.Background(), &bot.GetChatMemberParams{
		ChatID: chat,
		UserID: user,
	})
}
