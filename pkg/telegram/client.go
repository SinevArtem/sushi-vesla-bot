package telegram

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Client struct {
	api *tgbotapi.BotAPI
}

func New(token string) (*Client, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("ошибка подключения к Telegram: %w", err)
	}

	return &Client{api: bot}, nil
}

func (c *Client) GetAPI() *tgbotapi.BotAPI {
	return c.api
}

func (c *Client) SendMessage(chatID int64, text string, parseMode string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	if parseMode != "" {
		msg.ParseMode = parseMode
	}
	_, err := c.api.Send(msg)
	return err
}

func (c *Client) SendPhoto(chatID int64, photoBytes []byte, caption string) error {
	photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{
		Name:  "horizon.png",
		Bytes: photoBytes,
	})
	if caption != "" {
		photo.Caption = caption
	}
	_, err := c.api.Send(photo)
	return err
}

func (c *Client) SendMessageWithButtons(chatID int64, text string, buttons tgbotapi.InlineKeyboardMarkup) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = buttons
	_, err := c.api.Send(msg)
	return err
}

func (c *Client) GetUpdatesChan() tgbotapi.UpdatesChannel {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	return c.api.GetUpdatesChan(u)
}
