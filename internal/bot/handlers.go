package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"sushi-vesla-bot/internal/config"
	"sushi-vesla-bot/internal/geo"
	"sushi-vesla-bot/internal/logger"
	"sushi-vesla-bot/internal/repository"
	"sushi-vesla-bot/internal/service"
	"sushi-vesla-bot/pkg/telegram"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Handlers struct {
	client         *telegram.Client
	routeFinder    *service.RouteFinder
	camRepo        *repository.CameraRepository
	mapGen         *geo.MapGenerator
	cfg            *config.Config
	startTimes     map[int64]time.Time
	userStates     map[int64]string
	cameraTempData map[int64]map[string]float64
}

func NewHandlers(client *telegram.Client, routeFinder *service.RouteFinder, camRepo *repository.CameraRepository) *Handlers {
	return &Handlers{
		client:         client,
		routeFinder:    routeFinder,
		camRepo:        camRepo,
		mapGen:         geo.NewMapGenerator(),
		cfg:            config.Load(),
		startTimes:     make(map[int64]time.Time),
		userStates:     make(map[int64]string),
		cameraTempData: make(map[int64]map[string]float64),
	}
}

func (h *Handlers) HandleLocation(msg *tgbotapi.Message) {
	start := time.Now()
	chatID := msg.Chat.ID
	userID := msg.From.ID

	lat := msg.Location.Latitude
	lon := msg.Location.Longitude

	logger.Log.Info("получена геолокация",
		"user_id", userID,
		"chat_id", chatID,
		"lat", lat,
		"lon", lon,
	)

	// Проверяем, не в режиме ли добавления камеры
	if state, exists := h.userStates[userID]; exists && state == "adding_camera" {
		h.handleAddCameraLocation(msg)
		return
	}

	// Обычный поиск
	h.client.SendMessage(chatID, fmt.Sprintf("🔍 Ищем участки в радиусе %d км...", h.cfg.SearchRadius/1000), "")

	// Находим участки
	ctx := context.Background()
	segments, err := h.routeFinder.FindLongestSegments(
		ctx,
		lat, lon,
		h.cfg.SearchRadius,
		h.cfg.MaxSegments,
	)

	if err != nil {
		logger.Log.Error("ошибка поиска участков", "error", err)
		h.client.SendMessage(chatID,
			fmt.Sprintf("❌ Не удалось найти участки: %v\n\n💡 Попробуйте в другом месте или добавьте камеры!", err),
			"")
		h.showMainMenu(chatID)
		return
	}

	if len(segments) == 0 {
		h.client.SendMessage(chatID,
			fmt.Sprintf("😕 В радиусе %d км не найдено участков без камер.\n\n📷 Помогите сообществу - добавьте камеры в вашем районе!",
				h.cfg.SearchRadius/1000),
			"")
		h.showMainMenu(chatID)
		return
	}

	// Формируем текст с маршрутами
	text := "🚤 *Найдены самые длинные участки без камер!*\n\n"

	for _, seg := range segments {
		timeMinutes := seg.DistanceKm / 90 * 60

		text += fmt.Sprintf(
			"*%d. %s* %s\n"+
				"📏 Длина: *%.1f км*\n"+
				"⏱ Время: *%.0f мин* (90 км/ч)\n"+
				"📍 От: %.6f, %.6f\n"+
				"📍 До: %.6f, %.6f\n\n",
			seg.Rank,
			h.getMedal(seg.Rank),
			seg.RoadName,
			seg.DistanceKm,
			timeMinutes,
			seg.StartLat, seg.StartLon,
			seg.EndLat, seg.EndLon,
		)
	}

	text += "👇 *Нажмите на кнопку с маршрутом для открытия в навигаторе:*"

	if err := h.client.SendMessage(chatID, text, "Markdown"); err != nil {
		logger.Log.Error("ошибка отправки текста", "error", err)
	}

	// Создаем кнопки с URL для каждого маршрута
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, seg := range segments {
		// Создаем ссылки
		yandexURL := buildYandexMapsLink(seg.StartLat, seg.StartLon, seg.EndLat, seg.EndLon)
		googleURL := buildGoogleMapsLink(seg.StartLat, seg.StartLon, seg.EndLat, seg.EndLon)

		// Кнопка с Яндекс Картами
		buttonText := fmt.Sprintf("%s Маршрут #%d (%.1f км) 🗺",
			h.getMedal(seg.Rank),
			seg.Rank,
			seg.DistanceKm,
		)

		row := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL(buttonText, yandexURL),
		)
		rows = append(rows, row)

		// Дополнительная кнопка с Google Maps (опционально)
		row2 := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL(fmt.Sprintf("📍 Google Maps #%d", seg.Rank), googleURL),
		)
		rows = append(rows, row2)
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	if err := h.client.SendMessageWithButtons(chatID, "🗺 Выберите маршрут:", keyboard); err != nil {
		logger.Log.Error("ошибка отправки кнопок", "error", err)
	}

	// Показываем главное меню
	h.showMainMenu(chatID)

	logger.Log.Info("ответ отправлен",
		"user_id", userID,
		"segments", len(segments),
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

func (h *Handlers) HandleText(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	text := msg.Text

	// Проверяем состояние пользователя (для добавления/удаления камер)
	state, exists := h.userStates[userID]

	if exists && state == "adding_camera" {
		h.handleAddCameraInput(msg)
		return
	}

	if exists && state == "adding_camera_speed" {
		h.handleCameraSpeedInput(msg)
		return
	}

	if exists && state == "deleting_camera" {
		h.handleDeleteCameraInput(msg)
		return
	}

	// Обработка команд с клавиатуры
	switch text {
	case "/start":
		h.handleStart(chatID)
	case "/help":
		h.handleHelp(chatID)
	case "🔍 Найти участки":
		h.client.SendMessage(chatID, "📍 Отправьте вашу геолокацию или координаты (широта,долгота)", "")
	case "📷 Добавить камеру":
		h.startAddCamera(chatID, userID)
	case "🗑 Удалить камеру":
		h.startDeleteCamera(chatID, userID)
	case "📋 Список камер":
		h.listCameras(chatID)
	default:
		// Проверяем, может это координаты?
		if strings.Contains(text, ",") && !strings.Contains(text, "/") {
			h.handleCoordinateSearch(chatID, text)
			return
		}
		h.client.SendMessage(chatID, "❓ Неизвестная команда. Используйте кнопки меню.", "")
	}
}

func (h *Handlers) handleCoordinateSearch(chatID int64, text string) {
	parts := strings.Split(text, ",")
	if len(parts) != 2 {
		h.client.SendMessage(chatID, "❌ Неверный формат. Используйте: `широта,долгота`\nПример: `56.240803,43.962597`", "Markdown")
		return
	}

	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		h.client.SendMessage(chatID, "❌ Неверный формат широты.", "")
		return
	}

	lon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		h.client.SendMessage(chatID, "❌ Неверный формат долготы.", "")
		return
	}

	// Создаем фейковое сообщение с геолокацией
	fakeMsg := &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: chatID},
		From: &tgbotapi.User{ID: 0},
		Location: &tgbotapi.Location{
			Latitude:  lat,
			Longitude: lon,
		},
	}

	h.HandleLocation(fakeMsg)
}

func (h *Handlers) handleStart(chatID int64) {
	startMsg := `🚤 *Суши вёсла!*

Привет! Я показываю самые длинные участки дорог без камер.

📱 *Как пользоваться:*
1. Отправьте мне *геолокацию* через кнопку "📍 Прикрепить"
2. Или введите координаты: ` + "`56.240803,43.962597`" + `
3. Получите список самых длинных участков

📷 *Управление камерами:*
• Добавьте новую камеру
• Удалите существующую камеру
• Посмотрите список всех камер

⚠️ *Важно:*
Сервис не призывает нарушать ПДД. Мы просто информируем о камерах контроля скорости.

🏁 *СУШИ ВЁСЛА!*`

	h.client.SendMessage(chatID, startMsg, "Markdown")
	h.showMainMenu(chatID)
}

func (h *Handlers) handleHelp(chatID int64) {
	helpMsg := `📖 *Помощь*

🔹 *Как найти участки:*
• Нажмите "🔍 Найти участки" и отправьте геолокацию
• Или просто введите координаты: ` + "`56.240803,43.962597`" + `

🔹 *Управление камерами:*
• "📷 Добавить камеру" - введите ` + "`широта,долгота,ограничение`" + `
• "🗑 Удалить камеру" - введите ` + "`широта,долгота`" + `
• "📋 Список камер" - увидеть все камеры

🔹 *Доступные команды:*
/start — главное меню
/help — эта справка

🌊 *Попутного ветра!*`

	h.client.SendMessage(chatID, helpMsg, "Markdown")
}

// showMainMenu - показывает главное меню с кнопками внизу
func (h *Handlers) showMainMenu(chatID int64) {
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔍 Найти участки"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📷 Добавить камеру"),
			tgbotapi.NewKeyboardButton("🗑 Удалить камеру"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📋 Список камер"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, "📱 *Главное меню*\n\nВыберите действие:")
	msg.ReplyMarkup = keyboard
	msg.ParseMode = "Markdown"
	h.client.GetAPI().Send(msg)
}

func (h *Handlers) listCameras(chatID int64) {
	ctx := context.Background()
	cameras, err := h.camRepo.GetAllCameras(ctx)
	if err != nil {
		logger.Log.Error("ошибка получения списка камер", "error", err)
		h.client.SendMessage(chatID, fmt.Sprintf("❌ Ошибка получения списка: %v", err), "")
		return
	}

	if len(cameras) == 0 {
		h.client.SendMessage(chatID, "📷 В базе данных нет камер.\n\nДобавьте первую камеру через кнопку 'Добавить камеру'!", "")
		return
	}

	msg := fmt.Sprintf("📷 *Все камеры в базе данных*\n\nВсего: *%d* камер\n\n", len(cameras))

	for i, c := range cameras {
		msg += fmt.Sprintf(
			"*%d.* ID: `%d`\n"+
				"📍 %.6f, %.6f\n"+
				"📏 Ограничение: %d км/ч\n"+
				"🛣 %s\n\n",
			i+1, c.ID, c.Lat, c.Lon, c.SpeedLimit, c.RoadName,
		)
	}

	h.client.SendMessage(chatID, msg, "Markdown")
}

func (h *Handlers) getMedal(rank int) string {
	switch rank {
	case 1:
		return "🥇"
	case 2:
		return "🥈"
	case 3:
		return "🥉"
	default:
		return "🏅"
	}
}

// buildYandexMapsLink строит ссылку на Яндекс Карты
func buildYandexMapsLink(startLat, startLon, endLat, endLon float64) string {
	return fmt.Sprintf(
		"https://yandex.ru/maps/?rtext=%.6f,%.6f~%.6f,%.6f&rtt=auto",
		startLat, startLon, endLat, endLon,
	)
}

// buildGoogleMapsLink строит ссылку на Google Maps
func buildGoogleMapsLink(startLat, startLon, endLat, endLon float64) string {
	return fmt.Sprintf(
		"https://www.google.com/maps/dir/%.6f,%.6f/%.6f,%.6f",
		startLat, startLon, endLat, endLon,
	)
}
