package bot

import (
	"fmt"
	"strconv"
	"strings"

	"sushi-vesla-bot/internal/logger"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type SchemaState struct {
	ChatID     int64
	MessageID  int
	SessionID  string
	CurrentIdx int
}

var schemaStates = make(map[int64]*SchemaState)

func (h *Handlers) HandleCallback(callback *tgbotapi.CallbackQuery) {
	chatID := callback.Message.Chat.ID
	data := callback.Data
	messageID := callback.Message.MessageID

	logger.Log.Info("получен callback",
		"chat_id", chatID,
		"data", data,
	)

	h.client.GetAPI().Send(tgbotapi.NewCallback(callback.ID, ""))

	// Выход из добавления камеры
	if data == "exit_add_camera" {
		logger.Log.Info("выход из добавления камеры", "chat_id", chatID)
		delete(h.userStates, callback.From.ID)
		h.client.SendMessage(chatID, "❌ Добавление камеры отменено.", "")
		h.showMainMenu(chatID)
		return
	}

	// Выход из удаления камеры
	if data == "exit_delete_camera" {
		logger.Log.Info("выход из удаления камеры", "chat_id", chatID)
		delete(h.userStates, callback.From.ID)
		h.client.SendMessage(chatID, "❌ Удаление камеры отменено.", "")
		h.showMainMenu(chatID)
		return
	}

	if strings.HasPrefix(data, "schema_nav_") {
		h.handleSchemaNavigation(chatID, messageID, data)
		return
	}

	if data == "close_schema" {
		h.handleCloseSchema(chatID, messageID)
		return
	}

	if strings.HasPrefix(data, "page_") {
		logger.Log.Info("обработка пагинации", "data", data)
		h.handlePageNavigation(chatID, messageID, data)
		return
	}

	if data == "new_search" {
		logger.Log.Info("новый поиск", "data", data)
		h.client.SendMessage(chatID, "📍 Отправьте новую геолокацию или координаты", "")
		return
	}

	if strings.HasPrefix(data, "schema_") {
		logger.Log.Info("обработка схемы", "data", data)
		h.handleSchema(chatID, messageID, data)
		return
	}

	h.client.SendMessage(chatID, "Выберите действие в главном меню:", "")
	h.showMainMenu(chatID)
}

// handlePageNavigation - навигация по страницам маршрутов
func (h *Handlers) handlePageNavigation(chatID int64, messageID int, data string) {
	parts := strings.Split(data, "_")
	if len(parts) != 3 {
		logger.Log.Error("неверный формат пагинации", "data", data)
		h.client.SendMessage(chatID, "❌ Ошибка навигации", "")
		return
	}

	sessionID := parts[1]
	page, err := strconv.Atoi(parts[2])
	if err != nil {
		logger.Log.Error("неверный номер страницы", "error", err)
		h.client.SendMessage(chatID, "❌ Ошибка навигации", "")
		return
	}

	sessionData, ok := h.sessionCache.GetSession(sessionID)
	if !ok {
		logger.Log.Error("сессия не найдена", "sessionID", sessionID)
		h.client.SendMessage(chatID, "❌ Сессия устарела. Выполните новый поиск.", "")
		return
	}

	sessionData.CurrentPage = page

	segments, _ := h.sessionCache.GetPage(sessionID, page)
	if len(segments) == 0 {
		h.client.SendMessage(chatID, "😕 Больше маршрутов нет.", "")
		return
	}

	totalPages := sessionData.TotalPages
	// totalCount := sessionData.TotalCount
	currentPage := sessionData.CurrentPage

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

	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	editMsg.ParseMode = "Markdown"
	editMsg.ReplyMarkup = &keyboard

	_, err = h.client.GetAPI().Send(editMsg)
	if err != nil {
		logger.Log.Error("ошибка редактирования сообщения", "error", err)
	}
}

// handleSchema - обработка кнопки "Схема"
func (h *Handlers) handleSchema(chatID int64, messageID int, data string) {
	logger.Log.Info("генерация схемы", "data", data)

	routeID := strings.TrimPrefix(data, "schema_")
	if routeID == data {
		logger.Log.Error("неверный формат данных схемы", "data", data)
		h.client.SendMessage(chatID, "❌ Ошибка в данных маршрута", "")
		return
	}

	segment, ok := h.routeCache.Get(routeID)
	if !ok {
		h.client.SendMessage(chatID, "❌ Маршрут устарел. Выполните поиск ещё раз.", "")
		return
	}

	// ОТПРАВЛЯЕМ СООБЩЕНИЕ О ГЕНЕРАЦИИ
	h.client.SendMessage(chatID, "🔄 Генерируем схему маршрута...", "")

	// Находим сессию и индекс маршрута
	sessionID := ""
	currentIdx := 0
	totalRoutes := 0

	for sid, sdata := range h.sessionCache.sessions {
		for i, seg := range sdata.Segments {
			if seg.StartCameraID == segment.StartCameraID && seg.EndCameraID == segment.EndCameraID {
				sessionID = sid
				currentIdx = i
				totalRoutes = len(sdata.Segments)
				break
			}
		}
		if sessionID != "" {
			break
		}
	}

	if sessionID == "" {
		h.client.SendMessage(chatID, "❌ Ошибка: сессия не найдена", "")
		return
	}

	if state, ok := schemaStates[chatID]; ok {
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, state.MessageID)
		h.client.GetAPI().Send(deleteMsg)
		delete(schemaStates, chatID)
	}

	imgBytes, err := h.mapGen.GenerateRouteImage(segment)
	if err != nil {
		logger.Log.Error("ошибка генерации схемы", "error", err)
		h.client.SendMessage(chatID, fmt.Sprintf("❌ Ошибка генерации схемы: %v", err), "")
		return
	}

	logger.Log.Info("схема сгенерирована", "size", len(imgBytes))

	yandexURL := buildYandexMapsLink(segment.StartLat, segment.StartLon, segment.EndLat, segment.EndLon)

	caption := fmt.Sprintf(
		"🗺 *Схема маршрута #%d*\n\n"+
			"📏 Длина: *%.1f км*\n"+
			"📍 От: `%.6f, %.6f`\n"+
			"📍 До: `%.6f, %.6f`\n\n"+
			"📊 %d/%d",
		segment.Rank,
		segment.DistanceKm,
		segment.StartLat, segment.StartLon,
		segment.EndLat, segment.EndLon,
		currentIdx+1, totalRoutes,
	)

	var rows [][]tgbotapi.InlineKeyboardButton
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonURL("🗺 Открыть в Яндекс Картах", yandexURL),
	))

	var navRow []tgbotapi.InlineKeyboardButton

	if currentIdx > 0 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(
			"◀️ Предыдущая",
			fmt.Sprintf("schema_nav_%s_%d", sessionID, currentIdx-1),
		))
	}

	if currentIdx < totalRoutes-1 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(
			"Следующая ▶️",
			fmt.Sprintf("schema_nav_%s_%d", sessionID, currentIdx+1),
		))
	}

	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("❌ Закрыть", "close_schema"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{
		Name:  "route.png",
		Bytes: imgBytes,
	})
	photo.Caption = caption
	photo.ParseMode = "Markdown"
	photo.ReplyMarkup = keyboard

	sentMsg, err := h.client.GetAPI().Send(photo)
	if err != nil {
		logger.Log.Error("ошибка отправки схемы", "error", err)
		h.client.SendMessage(chatID, "❌ Не удалось отправить схему", "")
		return
	}

	schemaStates[chatID] = &SchemaState{
		ChatID:     chatID,
		MessageID:  sentMsg.MessageID,
		SessionID:  sessionID,
		CurrentIdx: currentIdx,
	}

	h.routeCache.Delete(routeID)
}

// handleSchemaNavigation - навигация по схемам (с кнопкой Яндекс Карт)
func (h *Handlers) handleSchemaNavigation(chatID int64, messageID int, data string) {
	parts := strings.Split(data, "_")
	if len(parts) != 4 {
		logger.Log.Error("неверный формат навигации по схемам", "data", data)
		h.client.SendMessage(chatID, "❌ Ошибка навигации", "")
		return
	}

	sessionID := parts[2]
	idx, err := strconv.Atoi(parts[3])
	if err != nil {
		logger.Log.Error("неверный индекс схемы", "error", err)
		h.client.SendMessage(chatID, "❌ Ошибка навигации", "")
		return
	}

	sessionData, ok := h.sessionCache.GetSession(sessionID)
	if !ok {
		logger.Log.Error("сессия не найдена", "sessionID", sessionID)
		h.client.SendMessage(chatID, "❌ Сессия устарела. Выполните новый поиск.", "")
		return
	}

	if idx < 0 || idx >= len(sessionData.Segments) {
		h.client.SendMessage(chatID, "❌ Схема не найдена", "")
		return
	}

	segment := sessionData.Segments[idx]

	if state, ok := schemaStates[chatID]; ok {
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, state.MessageID)
		h.client.GetAPI().Send(deleteMsg)
		delete(schemaStates, chatID)
	}

	imgBytes, err := h.mapGen.GenerateRouteImage(segment)
	if err != nil {
		logger.Log.Error("ошибка генерации схемы", "error", err)
		h.client.SendMessage(chatID, fmt.Sprintf("❌ Ошибка генерации схемы: %v", err), "")
		return
	}

	logger.Log.Info("схема сгенерирована", "size", len(imgBytes))

	// Ссылка на Яндекс Карты для этого маршрута
	yandexURL := buildYandexMapsLink(segment.StartLat, segment.StartLon, segment.EndLat, segment.EndLon)

	caption := fmt.Sprintf(
		"🗺 *Схема маршрута #%d*\n\n"+
			"📏 Длина: *%.1f км*\n"+
			"📍 От: `%.6f, %.6f`\n"+
			"📍 До: `%.6f, %.6f`\n\n"+
			"📊 %d/%d",
		segment.Rank,
		segment.DistanceKm,
		segment.StartLat, segment.StartLon,
		segment.EndLat, segment.EndLon,
		idx+1, len(sessionData.Segments),
	)

	var rows [][]tgbotapi.InlineKeyboardButton

	// Кнопка "Открыть в Яндекс Картах"
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonURL("🗺 Открыть в Яндекс Картах", yandexURL),
	))

	var navRow []tgbotapi.InlineKeyboardButton

	if idx > 0 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(
			"◀️ Предыдущая",
			fmt.Sprintf("schema_nav_%s_%d", sessionID, idx-1),
		))
	}

	if idx < len(sessionData.Segments)-1 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(
			"Следующая ▶️",
			fmt.Sprintf("schema_nav_%s_%d", sessionID, idx+1),
		))
	}

	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("❌ Закрыть", "close_schema"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{
		Name:  "route.png",
		Bytes: imgBytes,
	})
	photo.Caption = caption
	photo.ParseMode = "Markdown"
	photo.ReplyMarkup = keyboard

	sentMsg, err := h.client.GetAPI().Send(photo)
	if err != nil {
		logger.Log.Error("ошибка отправки схемы", "error", err)
		h.client.SendMessage(chatID, "❌ Не удалось отправить схему", "")
		return
	}

	schemaStates[chatID] = &SchemaState{
		ChatID:     chatID,
		MessageID:  sentMsg.MessageID,
		SessionID:  sessionID,
		CurrentIdx: idx,
	}
}

// handleCloseSchema - закрытие схемы
func (h *Handlers) handleCloseSchema(chatID int64, messageID int) {
	logger.Log.Info("закрытие схемы", "chat_id", chatID)

	if state, ok := schemaStates[chatID]; ok {
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, state.MessageID)
		h.client.GetAPI().Send(deleteMsg)
		delete(schemaStates, chatID)
	}

	if messageID != 0 {
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, messageID)
		h.client.GetAPI().Send(deleteMsg)
	}

	h.client.SendMessage(chatID, "✅ Схема закрыта", "")
	h.showMainMenu(chatID)
}
