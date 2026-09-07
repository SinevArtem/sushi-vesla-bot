package main

import (
	"os"
	"os/signal"
	"syscall"

	"sushi-vesla-bot/internal/bot"
	"sushi-vesla-bot/internal/config"
	"sushi-vesla-bot/internal/logger"
	"sushi-vesla-bot/internal/repository"
	"sushi-vesla-bot/internal/routing"
	"sushi-vesla-bot/internal/service"
	"sushi-vesla-bot/pkg/telegram"
)

func main() {
	// Загрузка конфигурации
	cfg := config.Load()

	// Инициализация логгера
	logger.Init(cfg.LogLevel)
	logger.Log.Info("🚀 Запуск бота СУШИ ВЁСЛА")

	// Подключение к БД
	camRepo, err := repository.NewCameraRepository(cfg.DatabaseURL)
	if err != nil {
		logger.Log.Error("ошибка подключения к БД", "error", err)
		os.Exit(1)
	}
	defer camRepo.Close()
	logger.Log.Info("✅ Подключение к БД установлено")

	// Инициализация клиента Telegram
	tgClient, err := telegram.New(cfg.TelegramToken)
	if err != nil {
		logger.Log.Error("ошибка инициализации Telegram", "error", err)
		os.Exit(1)
	}
	logger.Log.Info("✅ Telegram клиент инициализирован")

	// Инициализация клиента OSRM
	router := routing.NewClient(cfg.OSRMURL)
	logger.Log.Info("✅ OSRM клиент инициализирован")

	// Инициализация сервиса поиска
	routeFinder := service.NewRouteFinder(camRepo, router)
	logger.Log.Info("✅ Сервис поиска инициализирован")

	// Инициализация обработчиков
	handlers := bot.NewHandlers(tgClient, routeFinder, camRepo)
	logger.Log.Info("✅ Обработчики инициализированы")

	// Запуск обработки сообщений
	updates := tgClient.GetUpdatesChan()

	go func() {
		for update := range updates {
			// Обработка callback запросов (нажатия на кнопки)
			if update.CallbackQuery != nil {
				handlers.HandleCallback(update.CallbackQuery)
				continue
			}

			if update.Message == nil {
				continue
			}

			if update.Message.Location != nil {
				handlers.HandleLocation(update.Message)
			} else if update.Message.Text != "" {
				handlers.HandleText(update.Message)
			}
		}
	}()

	logger.Log.Info("✅ Бот запущен и готов к работе!")

	// Ожидание сигнала завершения
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Log.Info("👋 Бот завершает работу")
}
