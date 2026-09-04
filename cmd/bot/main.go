package main

import (
	"log"

	"sushi-vesla-bot/internal/bot"
	"sushi-vesla-bot/internal/config"
	"sushi-vesla-bot/internal/geo"
	"sushi-vesla-bot/internal/logger"
	"sushi-vesla-bot/pkg/telegram"
)

func main() {
	// Загружаем конфиг
	cfg := config.Load()

	// Инициализируем логгер
	logger.Init(cfg.LogLevel)
	logger.Log.Info("запуск бота", "environment", cfg.Environment)

	// Создаём Telegram клиент
	tgClient, err := telegram.New(cfg.TelegramToken)
	if err != nil {
		logger.Log.Error("ошибка создания клиента", "error", err)
		log.Fatal(err)
	}

	logger.Log.Info("бот авторизован", "username", tgClient.GetAPI().Self.UserName)

	// Сервис расстояний (пока фейковый)
	distSvc := geo.NewFakeDistanceService()

	// Создаём обработчики
	handlers := bot.NewHandlers(tgClient, distSvc)

	// Получаем обновления
	updates := tgClient.GetUpdatesChan()

	// Основной цикл
	for update := range updates {
		if update.Message == nil {
			continue
		}

		// Обрабатываем геолокацию
		if update.Message.Location != nil {
			go handlers.HandleLocation(update.Message) // в отдельной горутине
			continue
		}

		// Обрабатываем текст
		if update.Message.Text != "" {
			handlers.HandleText(update.Message)
		}
	}
}
