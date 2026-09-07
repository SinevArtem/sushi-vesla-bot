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
	mapGen         *geo.StaticMapGenerator
	cfg            *config.Config
	routeCache     *RouteCache
	sessionCache   *SessionCache
	pageStates     map[int64]*PageState
	startTimes     map[int64]time.Time
	userStates     map[int64]string
	cameraTempData map[int64]map[string]float64
}

type PageState struct {
	MessageID   int
	SessionID   string
	CurrentPage int
	TotalPages  int
}

func NewHandlers(client *telegram.Client, routeFinder *service.RouteFinder, camRepo *repository.CameraRepository) *Handlers {
	return &Handlers{
		client:         client,
		routeFinder:    routeFinder,
		camRepo:        camRepo,
		mapGen:         geo.NewStaticMapGenerator(),
		cfg:            config.Load(),
		routeCache:     NewRouteCache(),
		sessionCache:   &SessionCache{sessions: make(map[string]*SessionData)},
		pageStates:     make(map[int64]*PageState),
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

	if state, exists := h.userStates[userID]; exists && state == "adding_camera" {
		h.handleAddCameraLocation(msg)
		return
	}

	h.client.SendMessage(chatID, fmt.Sprintf("🔍 Ищем участки в радиусе %d км...", h.cfg.SearchRadius/1000), "")

	ctx := context.Background()

	allSegments, err := h.routeFinder.FindLongestSegmentsParallel(
		ctx,
		lat, lon,
		h.cfg.SearchRadius,
		10,
	)

	if err != nil {
		logger.Log.Error("ошибка поиска участков", "error", err)
		h.client.SendMessage(chatID,
			fmt.Sprintf("❌ Не удалось найти участки: %v\n\n💡 Попробуйте в другом месте или добавьте камеры!", err),
			"")
		h.showMainMenu(chatID)
		return
	}

	if len(allSegments) == 0 {
		h.client.SendMessage(chatID,
			fmt.Sprintf("😕 В радиусе %d км не найдено участков без камер.\n\n📷 Помогите сообществу - добавьте камеры в вашем районе!",
				h.cfg.SearchRadius/1000),
			"")
		h.showMainMenu(chatID)
		return
	}

	sessionID := h.sessionCache.SaveSession(chatID, allSegments)
	h.showRoutePage(chatID, sessionID, 0)
	h.showMainMenu(chatID)

	logger.Log.Info("ответ отправлен",
		"user_id", userID,
		"total_segments", len(allSegments),
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

func (h *Handlers) showRoutePage(chatID int64, sessionID string, page int) {
	segments, ok := h.sessionCache.GetPage(sessionID, page)
	if !ok || len(segments) == 0 {
		h.client.SendMessage(chatID, "😕 Больше маршрутов нет.", "")
		return
	}

	sessionData, _ := h.sessionCache.GetSession(sessionID)
	currentPage := sessionData.CurrentPage
	totalPages := sessionData.TotalPages
	// totalCount := sessionData.TotalCount

	text := "🚤 *Найдены самые длинные участки без камер!*\n\n"

	for rankOffset, seg := range segments {
		actualRank := currentPage*3 + rankOffset + 1

		medal := ""
		switch actualRank {
		case 1:
			medal = "🥇 "
		case 2:
			medal = "🥈 "
		case 3:
			medal = "🥉 "
		default:
			medal = fmt.Sprintf("%d. ", actualRank)
		}

		text += fmt.Sprintf(
			"*%s* %s\n"+
				"📏 Длина: *%.1f км*\n"+
				"📍 От: `%.6f, %.6f`\n"+
				"📍 До: `%.6f, %.6f`\n\n",
			medal,
			seg.RoadName,
			seg.DistanceKm,
			seg.StartLat, seg.StartLon,
			seg.EndLat, seg.EndLon,
		)
	}

	text += "👇 *Нажмите на кнопку с маршрутом для открытия в навигаторе:*"

	if totalPages > 1 {
		text += fmt.Sprintf("\n\n📊 Страница *%d/%d*", currentPage+1, totalPages)
	}

	var rows [][]tgbotapi.InlineKeyboardButton

	for rankOffset, seg := range segments {
		actualRank := currentPage*3 + rankOffset + 1
		routeID := h.routeCache.Save(seg)

		yandexURL := buildYandexMapsLink(seg.StartLat, seg.StartLon, seg.EndLat, seg.EndLon)
		buttonText := fmt.Sprintf("🗺 Маршрут #%d (%.1f км)", actualRank, seg.DistanceKm)

		row1 := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL(buttonText, yandexURL),
		)
		rows = append(rows, row1)

		schemaText := fmt.Sprintf("📊 Схема #%d", actualRank)
		schemaData := fmt.Sprintf("schema_%s", routeID)

		row2 := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(schemaText, schemaData),
		)
		rows = append(rows, row2)
	}

	var navRow []tgbotapi.InlineKeyboardButton

	if currentPage > 0 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("◀️ Назад", fmt.Sprintf("page_%s_%d", sessionID, currentPage-1)))
	}

	if currentPage < totalPages-1 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("Ещё маршруты ▶️", fmt.Sprintf("page_%s_%d", sessionID, currentPage+1)))
	}

	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔄 Новый поиск", "new_search"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	if state, ok := h.pageStates[chatID]; ok && state.SessionID == sessionID {
		editMsg := tgbotapi.NewEditMessageText(chatID, state.MessageID, text)
		editMsg.ParseMode = "Markdown"
		editMsg.ReplyMarkup = &keyboard

		_, err := h.client.GetAPI().Send(editMsg)
		if err != nil {
			logger.Log.Error("ошибка редактирования сообщения", "error", err)
			h.sendNewRoutePage(chatID, text, keyboard, sessionID, currentPage, totalPages)
		}
	} else {
		h.sendNewRoutePage(chatID, text, keyboard, sessionID, currentPage, totalPages)
	}
}

func (h *Handlers) sendNewRoutePage(chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup, sessionID string, currentPage, totalPages int) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	sentMsg, err := h.client.GetAPI().Send(msg)
	if err != nil {
		logger.Log.Error("ошибка отправки маршрутов", "error", err)
		return
	}

	h.pageStates[chatID] = &PageState{
		MessageID:   sentMsg.MessageID,
		SessionID:   sessionID,
		CurrentPage: currentPage,
		TotalPages:  totalPages,
	}
}

func (h *Handlers) HandleText(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	text := msg.Text

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
				"📍 `%.6f, %.6f`\n"+
				"📏 Ограничение: %d км/ч\n"+
				"🛣 %s\n\n",
			i+1, c.ID, c.Lat, c.Lon, c.SpeedLimit, c.RoadName,
		)
	}

	h.client.SendMessage(chatID, msg, "Markdown")
}

func buildYandexMapsLink(startLat, startLon, endLat, endLon float64) string {
	return fmt.Sprintf(
		"https://yandex.ru/maps/?rtext=%.6f,%.6f~%.6f,%.6f&rtt=auto",
		startLat, startLon, endLat, endLon,
	)
}

// startAddCamera - начало добавления камеры
func (h *Handlers) startAddCamera(chatID int64, userID int64) {
	h.userStates[userID] = "adding_camera"

	msg := `📷 *Добавление новой камеры*

Введите координаты камеры в формате:
` + "`широта,долгота,ограничение_скорости`" + `

📝 *Пример:*
` + "`56.240803,43.962597,60`" + `

ℹ️ Ограничение скорости указывается в км/ч.
Если ограничение неизвестно, укажите 0.

📍 Или отправьте *геолокацию* камеры.

⚠️ *Важно:* Камера должна находиться на расстоянии не менее 200 м от других камер.

Для выхода нажмите кнопку ниже:`

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Выйти", "exit_add_camera"),
		),
	)

	h.client.SendMessageWithButtons(chatID, msg, keyboard)
}

func (h *Handlers) handleAddCameraInput(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	text := msg.Text

	if msg.Location != nil {
		h.handleAddCameraLocation(msg)
		return
	}

	parts := strings.Split(text, ",")
	if len(parts) != 3 {
		// Показываем ошибку с кнопкой "Выйти"
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Выйти", "exit_add_camera"),
			),
		)
		h.client.SendMessageWithButtons(chatID,
			"❌ Неверный формат. Используйте: `широта,долгота,ограничение_скорости`\n\nПример: `56.240803,43.962597,60`",
			keyboard)
		return
	}

	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Выйти", "exit_add_camera"),
			),
		)
		h.client.SendMessageWithButtons(chatID, "❌ Неверный формат широты. Используйте число (например, 56.240803)", keyboard)
		return
	}

	lon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Выйти", "exit_add_camera"),
			),
		)
		h.client.SendMessageWithButtons(chatID, "❌ Неверный формат долготы. Используйте число (например, 43.962597)", keyboard)
		return
	}

	speedLimit, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Выйти", "exit_add_camera"),
			),
		)
		h.client.SendMessageWithButtons(chatID, "❌ Неверный формат ограничения скорости. Используйте число (например, 60)", keyboard)
		return
	}

	h.addCameraToDB(chatID, userID, lat, lon, speedLimit)
}

func (h *Handlers) handleAddCameraLocation(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	lat := msg.Location.Latitude
	lon := msg.Location.Longitude

	h.userStates[userID] = "adding_camera_speed"
	h.client.SendMessage(chatID,
		fmt.Sprintf("📍 Получены координаты: %.6f, %.6f\n\nВведите ограничение скорости (в км/ч) или 0, если неизвестно:", lat, lon),
		"")

	h.cameraTempData[userID] = map[string]float64{
		"lat": lat,
		"lon": lon,
	}
}

func (h *Handlers) handleCameraSpeedInput(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	text := msg.Text

	speedLimit, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		h.client.SendMessage(chatID, "❌ Неверный формат. Введите число (например, 60)", "")
		return
	}

	data, exists := h.cameraTempData[userID]
	if !exists {
		h.client.SendMessage(chatID, "❌ Ошибка: координаты не найдены. Попробуйте снова.", "")
		delete(h.userStates, userID)
		return
	}

	lat := data["lat"]
	lon := data["lon"]

	h.addCameraToDB(chatID, userID, lat, lon, speedLimit)
}

func (h *Handlers) addCameraToDB(chatID int64, userID int64, lat, lon float64, speedLimit int) {
	ctx := context.Background()

	hasNearby, nearbyCam, err := h.camRepo.CheckCameraNearby(ctx, lat, lon, 200.0)
	if err != nil {
		logger.Log.Error("ошибка проверки камеры", "error", err)
		h.client.SendMessage(chatID, fmt.Sprintf("❌ Ошибка проверки: %v", err), "")
		return
	}

	if hasNearby {
		msg := fmt.Sprintf(
			"❌ *Добавление запрещено!*\n\n"+
				"Рядом уже есть камера:\n"+
				"📍 ID: %d\n"+
				"📍 Координаты: `%.6f, %.6f`\n"+
				"📏 Ограничение: %d км/ч\n"+
				"🛣 %s\n\n"+
				"Расстояние менее 200 метров. Добавление невозможно.",
			nearbyCam.ID,
			nearbyCam.Lat, nearbyCam.Lon,
			nearbyCam.SpeedLimit,
			nearbyCam.RoadName,
		)
		h.client.SendMessage(chatID, msg, "Markdown")
		return
	}

	camera := repository.Camera{
		Lat:        lat,
		Lon:        lon,
		SpeedLimit: speedLimit,
		RoadName:   "Добавлена пользователем",
	}

	id, err := h.camRepo.AddCamera(ctx, camera)
	if err != nil {
		logger.Log.Error("ошибка добавления камеры", "error", err)
		h.client.SendMessage(chatID, fmt.Sprintf("❌ Ошибка добавления камеры: %v", err), "")
		return
	}

	delete(h.userStates, userID)
	delete(h.cameraTempData, userID)

	responseMsg := fmt.Sprintf(
		"✅ *Камера успешно добавлена!*\n\n"+
			"📍 Координаты: `%.6f, %.6f`\n"+
			"📏 Ограничение: %d км/ч\n"+
			"🆔 ID: %d",
		lat, lon, speedLimit, id,
	)

	h.client.SendMessage(chatID, responseMsg, "Markdown")
	h.showMainMenu(chatID)
}

func (h *Handlers) startDeleteCamera(chatID int64, userID int64) {
	h.userStates[userID] = "deleting_camera"

	msg := `🗑 *Удаление камеры*

Введите координаты камеры, которую хотите удалить:
` + "`широта,долгота`" + `

📝 *Пример:*
` + "`56.240803,43.962597`" + `

ℹ️ Будет удалена камера с указанными координатами.

Для выхода нажмите кнопку ниже:`

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Выйти", "exit_delete_camera"),
		),
	)

	h.client.SendMessageWithButtons(chatID, msg, keyboard)
}

func (h *Handlers) handleDeleteCameraInput(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	text := msg.Text

	parts := strings.Split(text, ",")
	if len(parts) != 2 {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Выйти", "exit_delete_camera"),
			),
		)
		h.client.SendMessageWithButtons(chatID,
			"❌ Неверный формат. Используйте: `широта,долгота`\n\nПример: `56.240803,43.962597`",
			keyboard)
		return
	}

	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Выйти", "exit_delete_camera"),
			),
		)
		h.client.SendMessageWithButtons(chatID, "❌ Неверный формат широты. Используйте число (например, 56.240803)", keyboard)
		return
	}

	lon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Выйти", "exit_delete_camera"),
			),
		)
		h.client.SendMessageWithButtons(chatID, "❌ Неверный формат долготы. Используйте число (например, 43.962597)", keyboard)
		return
	}

	if lat < -90 || lat > 90 {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Выйти", "exit_delete_camera"),
			),
		)
		h.client.SendMessageWithButtons(chatID, "❌ Широта должна быть в диапазоне от -90 до 90", keyboard)
		return
	}
	if lon < -180 || lon > 180 {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Выйти", "exit_delete_camera"),
			),
		)
		h.client.SendMessageWithButtons(chatID, "❌ Долгота должна быть в диапазоне от -180 до 180", keyboard)
		return
	}

	ctx := context.Background()
	cameras, err := h.camRepo.GetCamerasByCoordinates(ctx, lat, lon)
	if err != nil {
		logger.Log.Error("ошибка поиска камеры", "error", err)
		h.client.SendMessage(chatID, fmt.Sprintf("❌ Ошибка поиска камеры: %v", err), "")
		return
	}

	if len(cameras) == 0 {
		h.client.SendMessage(chatID, "❌ Камера с указанными координатами не найдена.", "")
		return
	}

	err = h.camRepo.DeleteCamera(ctx, cameras[0].ID)
	if err != nil {
		logger.Log.Error("ошибка удаления камеры", "error", err)
		h.client.SendMessage(chatID, fmt.Sprintf("❌ Ошибка удаления камеры: %v", err), "")
		return
	}

	delete(h.userStates, userID)

	responseMsg := fmt.Sprintf(
		"✅ *Камера успешно удалена!*\n\n"+
			"📍 Координаты: `%.6f, %.6f`\n"+
			"📏 Ограничение: %d км/ч\n"+
			"🆔 ID: %d",
		cameras[0].Lat, cameras[0].Lon, cameras[0].SpeedLimit, cameras[0].ID,
	)

	h.client.SendMessage(chatID, responseMsg, "Markdown")
	h.showMainMenu(chatID)
}
