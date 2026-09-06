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

	// Отвечаем на callback (убираем "часики")
	h.client.GetAPI().Send(tgbotapi.NewCallback(callback.ID, ""))

	// Обработка кнопки "Копировать координаты"
	if strings.HasPrefix(data, "coords_") {
		h.handleCopyCoords(chatID, data)
		return
	}

	// Сейчас у нас нет других callback-кнопок
	h.client.SendMessage(chatID, "Выберите действие в главном меню:", "")
	h.showMainMenu(chatID)
}

func (h *Handlers) handleCopyCoords(chatID int64, data string) {
	// Парсим данные: coords_1_55.7558_37.6173_55.7658_37.6273
	parts := strings.Split(data, "_")
	if len(parts) != 7 {
		h.client.SendMessage(chatID, "❌ Ошибка в данных координат", "")
		return
	}

	rank := parts[1]
	startLat, _ := strconv.ParseFloat(parts[2], 64)
	startLon, _ := strconv.ParseFloat(parts[3], 64)
	endLat, _ := strconv.ParseFloat(parts[4], 64)
	endLon, _ := strconv.ParseFloat(parts[5], 64)

	// Форматируем координаты единым стилем
	coordsText := formatCoordinates(
		rank,
		startLat, startLon,
		endLat, endLon,
	)

	h.client.SendMessage(chatID, coordsText, "Markdown")

	// Кнопка для быстрого копирования
	shareText := fmt.Sprintf(
		"Маршрут #%s: %.6f,%.6f → %.6f,%.6f",
		rank, startLat, startLon, endLat, endLon,
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonSwitch(
				"📤 Скопировать всё",
				shareText,
			),
		),
	)

	h.client.SendMessageWithButtons(chatID, "Или нажмите кнопку, чтобы скопировать всё:", keyboard)
}

// formatCoordinates - единый формат для всех координат
func formatCoordinates(rank string, startLat, startLon, endLat, endLon float64) string {
	return fmt.Sprintf(
		"📍 *Координаты маршрута #%s*\n\n"+
			"🚩 *Старт:*\n"+
			"`%.6f, %.6f`\n\n"+
			"🏁 *Финиш:*\n"+
			"`%.6f, %.6f`\n\n"+
			"📋 *Нажмите на координаты и выберите Копировать*",
		rank, startLat, startLon, endLat, endLon,
	)
}

// formatCoordinatesSimple - упрощенный формат для списка маршрутов
func formatCoordinatesSimple(startLat, startLon, endLat, endLon float64) string {
	return fmt.Sprintf(
		"📍 От: `%.6f, %.6f`\n"+
			"📍 До: `%.6f, %.6f`",
		startLat, startLon, endLat, endLon,
	)
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

	// Проверяем отмену
	if text == "/cancel" {
		delete(h.userStates, userID)
		h.client.SendMessage(chatID, "❌ Добавление камеры отменено.", "")
		h.showMainMenu(chatID)
		return
	}

	// Если это геолокация
	if msg.Location != nil {
		h.handleAddCameraLocation(msg)
		return
	}

	// Парсим ввод
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

	// Добавляем камеру
	h.addCameraToDB(chatID, userID, lat, lon, speedLimit)
}

func (h *Handlers) handleAddCameraLocation(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	lat := msg.Location.Latitude
	lon := msg.Location.Longitude

	// Запрашиваем ограничение скорости
	h.userStates[userID] = "adding_camera_speed"
	h.client.SendMessage(chatID,
		fmt.Sprintf("📍 Получены координаты: %.6f, %.6f\n\nВведите ограничение скорости (в км/ч) или 0, если неизвестно:", lat, lon),
		"")

	// Сохраняем координаты во временное состояние
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

	// Проверяем отмену
	if text == "/cancel" {
		delete(h.userStates, userID)
		h.client.SendMessage(chatID, "❌ Удаление камеры отменено.", "")
		h.showMainMenu(chatID)
		return
	}

	// Парсим ввод
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

	// Проверяем координаты
	if lat < -90 || lat > 90 {
		h.client.SendMessage(chatID, "❌ Широта должна быть в диапазоне от -90 до 90", "")
		return
	}
	if lon < -180 || lon > 180 {
		h.client.SendMessage(chatID, "❌ Долгота должна быть в диапазоне от -180 до 180", "")
		return
	}

	// Ищем камеру
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

	// Удаляем камеру
	err = h.camRepo.DeleteCamera(ctx, cameras[0].ID)
	if err != nil {
		logger.Log.Error("ошибка удаления камеры", "error", err)
		h.client.SendMessage(chatID, fmt.Sprintf("❌ Ошибка удаления камеры: %v", err), "")
		return
	}

	// Очищаем состояние
	delete(h.userStates, userID)

	// Подтверждение с единым форматом координат
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
