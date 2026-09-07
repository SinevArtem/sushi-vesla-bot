package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"sushi-vesla-bot/internal/logger"
	"sushi-vesla-bot/internal/repository"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (h *Handlers) HandleCallback(callback *tgbotapi.CallbackQuery) {
	chatID := callback.Message.Chat.ID
	data := callback.Data

	logger.Log.Info("получен callback",
		"chat_id", chatID,
		"data", data,
	)

	// Отвечаем на callback (убираем "часики")
	h.client.GetAPI().Send(tgbotapi.NewCallback(callback.ID, ""))

	// Обработка кнопки "Схема"
	if strings.HasPrefix(data, "schema_") {
		logger.Log.Info("обработка схемы", "data", data)
		h.handleSchema(chatID, data)
		return
	}

	// Обработка кнопки "Координаты" (если осталась старая)
	if strings.HasPrefix(data, "coords_") {
		logger.Log.Info("обработка координат", "data", data)
		h.handleCopyCoords(chatID, data)
		return
	}

	h.client.SendMessage(chatID, "Выберите действие в главном меню:", "")
	h.showMainMenu(chatID)
}

// handleSchema - обработка кнопки "Схема" - отправляет картинку с маршрутом
func (h *Handlers) handleSchema(chatID int64, data string) {
	logger.Log.Info("генерация схемы", "data", data)

	// Парсим данные: schema_1_56.298795_43.958443_56.242684_43.964236
	parts := strings.Split(data, "_")
	if len(parts) != 6 {
		logger.Log.Error("неверный формат данных схемы", "parts", len(parts))
		h.client.SendMessage(chatID, "❌ Ошибка в данных маршрута", "")
		return
	}

	rank := parts[1]
	startLat, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		logger.Log.Error("ошибка парсинга широты старта", "error", err)
		h.client.SendMessage(chatID, "❌ Ошибка в данных маршрута", "")
		return
	}
	startLon, err := strconv.ParseFloat(parts[3], 64)
	if err != nil {
		logger.Log.Error("ошибка парсинга долготы старта", "error", err)
		h.client.SendMessage(chatID, "❌ Ошибка в данных маршрута", "")
		return
	}
	endLat, err := strconv.ParseFloat(parts[4], 64)
	if err != nil {
		logger.Log.Error("ошибка парсинга широты финиша", "error", err)
		h.client.SendMessage(chatID, "❌ Ошибка в данных маршрута", "")
		return
	}
	endLon, err := strconv.ParseFloat(parts[5], 64)
	if err != nil {
		logger.Log.Error("ошибка парсинга долготы финиша", "error", err)
		h.client.SendMessage(chatID, "❌ Ошибка в данных маршрута", "")
		return
	}

	logger.Log.Info("данные маршрута для схемы",
		"rank", rank,
		"startLat", startLat,
		"startLon", startLon,
		"endLat", endLat,
		"endLon", endLon,
	)

	// Отправляем сообщение о генерации схемы
	h.client.SendMessage(chatID, "🔄 Генерирую схему маршрута...", "")

	// Получаем геометрию маршрута от OSRM
	ctx := context.Background()
	route, err := h.routeFinder.GetRoute(ctx, startLat, startLon, endLat, endLon)
	if err != nil {
		logger.Log.Error("ошибка получения маршрута от OSRM", "error", err)
		h.client.SendMessage(chatID, fmt.Sprintf("❌ Не удалось получить маршрут: %v", err), "")
		return
	}

	// Генерируем картинку с маршрутом, передавая геометрию
	imgBytes, err := h.mapGen.GenerateRouteImage(startLat, startLon, endLat, endLon, route.Geometry)
	if err != nil {
		logger.Log.Error("ошибка генерации схемы", "error", err)
		h.client.SendMessage(chatID, fmt.Sprintf("❌ Ошибка генерации схемы: %v", err), "")
		return
	}

	logger.Log.Info("схема сгенерирована", "size", len(imgBytes))

	// Отправляем картинку
	caption := fmt.Sprintf("🗺 *Схема маршрута #%s*\n\n📍 От: %.6f, %.6f\n📍 До: %.6f, %.6f\n📏 %.1f км",
		rank, startLat, startLon, endLat, endLon, route.Distance/1000.0)

	if err := h.client.SendPhoto(chatID, imgBytes, caption); err != nil {
		logger.Log.Error("ошибка отправки схемы", "error", err)
		h.client.SendMessage(chatID, "❌ Не удалось отправить схему", "")
	}
}

// handleCopyCoords - обработка кнопки "Координаты"
func (h *Handlers) handleCopyCoords(chatID int64, data string) {
	parts := strings.Split(data, "_")
	if len(parts) != 6 {
		h.client.SendMessage(chatID, "❌ Ошибка в данных координат", "")
		return
	}

	rank := parts[1]
	startLat, _ := strconv.ParseFloat(parts[2], 64)
	startLon, _ := strconv.ParseFloat(parts[3], 64)
	endLat, _ := strconv.ParseFloat(parts[4], 64)
	endLon, _ := strconv.ParseFloat(parts[5], 64)

	coordsText := fmt.Sprintf(
		"📍 *Координаты маршрута #%s*\n\n"+
			"🚩 *Старт:*\n"+
			"`%.6f, %.6f`\n\n"+
			"🏁 *Финиш:*\n"+
			"`%.6f, %.6f`\n\n"+
			"📋 *Нажмите на координаты и выберите Копировать*",
		rank, startLat, startLon, endLat, endLon,
	)

	h.client.SendMessage(chatID, coordsText, "Markdown")
}

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

Для отмены введите /cancel`

	h.client.SendMessage(chatID, msg, "Markdown")
}

func (h *Handlers) handleAddCameraInput(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	text := msg.Text

	if text == "/cancel" {
		delete(h.userStates, userID)
		h.client.SendMessage(chatID, "❌ Добавление камеры отменено.", "")
		h.showMainMenu(chatID)
		return
	}

	if msg.Location != nil {
		h.handleAddCameraLocation(msg)
		return
	}

	parts := strings.Split(text, ",")
	if len(parts) != 3 {
		h.client.SendMessage(chatID,
			"❌ Неверный формат. Используйте: `широта,долгота,ограничение_скорости`\n\nПример: `56.240803,43.962597,60`",
			"Markdown")
		return
	}

	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		h.client.SendMessage(chatID, "❌ Неверный формат широты. Используйте число (например, 56.240803)", "")
		return
	}

	lon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		h.client.SendMessage(chatID, "❌ Неверный формат долготы. Используйте число (например, 43.962597)", "")
		return
	}

	speedLimit, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil {
		h.client.SendMessage(chatID, "❌ Неверный формат ограничения скорости. Используйте число (например, 60)", "")
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

	if text == "/cancel" {
		delete(h.userStates, userID)
		delete(h.cameraTempData, userID)
		h.client.SendMessage(chatID, "❌ Добавление камеры отменено.", "")
		h.showMainMenu(chatID)
		return
	}

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
	camera := repository.Camera{
		Lat:        lat,
		Lon:        lon,
		SpeedLimit: speedLimit,
		RoadName:   "Добавлена пользователем",
	}

	ctx := context.Background()
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

Для отмены введите /cancel`

	h.client.SendMessage(chatID, msg, "Markdown")
}

func (h *Handlers) handleDeleteCameraInput(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	text := msg.Text

	if text == "/cancel" {
		delete(h.userStates, userID)
		h.client.SendMessage(chatID, "❌ Удаление камеры отменено.", "")
		h.showMainMenu(chatID)
		return
	}

	parts := strings.Split(text, ",")
	if len(parts) != 2 {
		h.client.SendMessage(chatID,
			"❌ Неверный формат. Используйте: `широта,долгота`\n\nПример: `56.240803,43.962597`",
			"Markdown")
		return
	}

	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		h.client.SendMessage(chatID, "❌ Неверный формат широты. Используйте число (например, 56.240803)", "")
		return
	}

	lon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		h.client.SendMessage(chatID, "❌ Неверный формат долготы. Используйте число (например, 43.962597)", "")
		return
	}

	if lat < -90 || lat > 90 {
		h.client.SendMessage(chatID, "❌ Широта должна быть в диапазоне от -90 до 90", "")
		return
	}
	if lon < -180 || lon > 180 {
		h.client.SendMessage(chatID, "❌ Долгота должна быть в диапазоне от -180 до 180", "")
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
