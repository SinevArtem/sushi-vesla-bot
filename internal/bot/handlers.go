package bot

import (
	"fmt"
	"time"

	"sushi-vesla-bot/internal/geo"
	"sushi-vesla-bot/internal/logger"
	"sushi-vesla-bot/pkg/telegram"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Handlers struct {
	client     *telegram.Client
	mapGen     *geo.MapGenerator
	distSvc    geo.DistanceService
	startTimes map[int64]time.Time // для замера времени ответа
}

func NewHandlers(client *telegram.Client, distSvc geo.DistanceService) *Handlers {
	return &Handlers{
		client:     client,
		mapGen:     geo.NewMapGenerator(),
		distSvc:    distSvc,
		startTimes: make(map[int64]time.Time),
	}
}

func (h *Handlers) HandleLocation(msg *tgbotapi.Message) {
	start := time.Now()
	chatID := msg.Chat.ID

	lat := msg.Location.Latitude
	lon := msg.Location.Longitude

	logger.Log.Info("получена геолокация",
		"chat_id", chatID,
		"lat", lat,
		"lon", lon,
	)

	// Получаем расстояние
	distanceKm, err := h.distSvc.GetMaxDistance(lat, lon)
	if err != nil {
		logger.Log.Error("ошибка получения расстояния", "error", err)
		h.client.SendMessage(chatID, "❌ Не удалось найти горизонт. Попробуйте позже.", "")
		return
	}

	// Время в пути при 90 км/ч
	timeMinutes := distanceKm / 90 * 60

	// 1. Отправляем текстовый ответ
	text := fmt.Sprintf(
		"🚤 *Вёсла готовы!*\n\n"+
			"📍 Ваши координаты: %.5f, %.5f\n"+
			"📏 До горизонта: *%.1f км*\n"+
			"⏱ Время в пути (90 км/ч): *%.0f мин*\n\n"+
			"🏁 *СУШИ ВЁСЛА!*",
		lat, lon, distanceKm, timeMinutes,
	)

	if err := h.client.SendMessage(chatID, text, "Markdown"); err != nil {
		logger.Log.Error("ошибка отправки текста", "error", err)
	}

	// 2. Генерируем и отправляем картинку
	imgBytes, err := h.mapGen.GenerateHorizonMap(lat, lon, distanceKm)
	if err != nil {
		logger.Log.Error("ошибка генерации карты", "error", err)
	} else {
		if err := h.client.SendPhoto(chatID, imgBytes, "🗺 Ваш горизонт на карте"); err != nil {
			logger.Log.Error("ошибка отправки фото", "error", err)
		}
	}

	// 3. Кнопка "Поделиться"
	shareBtn := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonSwitch(
				"📤 Поделиться горизонтом",
				"",
			),
		),
	)

	if err := h.client.SendMessageWithButtons(chatID, "Расскажите друзьям о своём горизонте!", shareBtn); err != nil {
		logger.Log.Error("ошибка отправки кнопки", "error", err)
	}

	// Логируем время ответа
	logger.Log.Info("ответ отправлен",
		"chat_id", chatID,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

func (h *Handlers) HandleText(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	text := msg.Text

	switch text {
	case "/start":
		h.handleStart(chatID)
	case "/help":
		h.handleHelp(chatID)
	default:
		h.client.SendMessage(chatID, "❓ Неизвестная команда. Используйте /start или отправьте геолокацию.", "")
	}
}

func (h *Handlers) handleStart(chatID int64) {
	startMsg := `🚤 *Суши вёсла!*

Привет! Я показываю самые длинные прямые участки дорог без камер.

📱 *Как пользоваться:*
1. Отправьте мне *геолокацию*
2. Я найду ближайший "горизонт"
3. Получите расстояние и карту

⚠️ *Важно:*
Сервис не призывает нарушать ПДД. Мы просто измеряем расстояния между камерами для планирования комфортной поездки.

🏁 *СУШИ ВЁСЛА!*`

	h.client.SendMessage(chatID, startMsg, "Markdown")
}

func (h *Handlers) handleHelp(chatID int64) {
	helpMsg := `📖 *Помощь*

🔹 *Как отправить геолокацию:*
Нажмите на скрепку 📎 → "Геолокация" → отправьте текущее место

🔹 *Доступные команды:*
/start — главное меню
/help — эта справка

🔹 *Обратная связь:*
Если нашли баг или есть идея — напишите @ваш_ник

🌊 *Попутного ветра!*`

	h.client.SendMessage(chatID, helpMsg, "Markdown")
}
