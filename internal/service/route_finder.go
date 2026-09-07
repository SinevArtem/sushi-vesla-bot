package service

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"sushi-vesla-bot/internal/repository"
	"sushi-vesla-bot/internal/routing"
)

const (
	CameraRadiusMeters       = 50.0
	IntermediateCameraRadius = 75.0
	MinClearDistanceMeters   = 100.0
)

type Segment struct {
	StartLat        float64              `json:"start_lat"`
	StartLon        float64              `json:"start_lon"`
	EndLat          float64              `json:"end_lat"`
	EndLon          float64              `json:"end_lon"`
	DistanceKm      float64              `json:"distance_km"`
	ClearDistanceKm float64              `json:"clear_distance_km"`
	DurationMin     float64              `json:"duration_min"`
	RoadName        string               `json:"road_name"`
	StartCameraID   int                  `json:"start_camera_id"`
	EndCameraID     int                  `json:"end_camera_id"`
	Rank            int                  `json:"rank"`
	Geometry        []routing.Coordinate `json:"geometry"`
}

type RouteFinder struct {
	camRepo *repository.CameraRepository
	router  *routing.Client
}

func NewRouteFinder(camRepo *repository.CameraRepository, router *routing.Client) *RouteFinder {
	return &RouteFinder{
		camRepo: camRepo,
		router:  router,
	}
}

func (f *RouteFinder) FindLongestSegments(ctx context.Context, lat, lon float64, radiusMeters int, limit int) ([]Segment, error) {
	startTime := time.Now()
	log.Printf("🔍 Поиск: lat=%.6f, lon=%.6f, radius=%d м", lat, lon, radiusMeters)

	// 1. Получаем камеры из БД
	cameras, err := f.camRepo.GetCamerasInRadius(ctx, lat, lon, radiusMeters)
	if err != nil {
		return nil, fmt.Errorf("получение камер: %w", err)
	}

	if len(cameras) < 2 {
		return nil, fmt.Errorf("найдено только %d камер, нужно минимум 2", len(cameras))
	}

	log.Printf("📷 Найдено камер: %d", len(cameras))
	for idx, cam := range cameras {
		log.Printf("  Камера ID=%d: (%.6f, %.6f) %s", cam.ID, cam.Lat, cam.Lon, cam.RoadName)
		_ = idx
	}

	// 2. Для каждой камеры получаем привязку к дороге через OSRM Nearest
	roadPoints := make([]routing.Coordinate, len(cameras))
	for i, camera := range cameras {
		nearest, err := f.router.Nearest(ctx, camera.Lat, camera.Lon)
		if err != nil {
			log.Printf("⚠️ Ошибка Nearest для камеры ID=%d: %v", camera.ID, err)
			roadPoints[i] = routing.Coordinate{Lat: camera.Lat, Lon: camera.Lon}
			continue
		}
		log.Printf("  Камера ID=%d привязана к дороге: (%.6f, %.6f), расстояние %.2f м",
			camera.ID, nearest.Coordinate.Lat, nearest.Coordinate.Lon, nearest.Distance)
		roadPoints[i] = nearest.Coordinate
	}

	// 3. Получаем матрицу дорожных расстояний через OSRM Table
	points := make([]routing.Coordinate, len(roadPoints))
	copy(points, roadPoints)

	log.Printf("📊 Запрос матрицы расстояний к OSRM...")
	matrix, err := f.router.Table(ctx, points)
	if err != nil {
		return nil, fmt.Errorf("получение матрицы маршрутов: %w", err)
	}

	// 4. Собираем кандидатов
	type Candidate struct {
		A        int
		B        int
		Distance float64 // в метрах
		Duration float64 // в секундах
	}

	var candidates []Candidate
	for i := 0; i < len(cameras); i++ {
		for j := i + 1; j < len(cameras); j++ {
			distance := matrix[i][j].Distance
			if distance <= 0 {
				continue
			}
			candidates = append(candidates, Candidate{
				A:        i,
				B:        j,
				Distance: distance,
				Duration: matrix[i][j].Duration,
			})
		}
	}

	// 5. Сортируем по убыванию расстояния
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Distance > candidates[j].Distance
	})

	log.Printf("📊 Найдено %d пар камер", len(candidates))

	// 6. Проходим по кандидатам - НОВАЯ ЛОГИКА
	var result []Segment
	checkedCount := 0
	skippedCount := 0

	for _, candidate := range candidates {
		checkedCount++
		camA := cameras[candidate.A]
		camB := cameras[candidate.B]
		roadA := roadPoints[candidate.A]
		roadB := roadPoints[candidate.B]

		log.Printf("🔍 Проверяем пару ID=%d и ID=%d (расстояние: %.2f км)",
			camA.ID, camB.ID, candidate.Distance/1000.0)

		// Строим маршрут между дорожными точками
		route, err := f.router.Route(ctx, roadA, roadB)
		if err != nil {
			log.Printf("⚠️ Ошибка маршрута %d -> %d: %v", camA.ID, camB.ID, err)
			continue
		}

		// Проверяем промежуточные камеры на маршруте (до камеры B)
		hasIntermediate, interIdx := f.findFirstIntermediateCamera(cameras, candidate.A, candidate.B, route.Geometry)
		if hasIntermediate {
			log.Printf("❌ Маршрут %d -> %d содержит промежуточную камеру ID=%d", camA.ID, camB.ID, cameras[interIdx].ID)
			skippedCount++
			continue
		}

		// Обрезаем маршрут при входе в зону камеры B
		cutGeometry, cutDistance, ok := routing.CutRouteAtCircle(
			route.Geometry,
			camB.Lat, camB.Lon,
			CameraRadiusMeters,
		)

		if !ok {
			log.Printf("⚠️ Не удалось обрезать маршрут %d -> %d", camA.ID, camB.ID)
			skippedCount++
			continue
		}

		if cutDistance < MinClearDistanceMeters {
			log.Printf("⚠️ Маршрут %d -> %d слишком короткий: %.0f м", camA.ID, camB.ID, cutDistance)
			skippedCount++
			continue
		}

		// Находим начало и конец обрезанного маршрута
		start := cutGeometry[0]
		end := cutGeometry[len(cutGeometry)-1]

		// Проверяем, что камера A не в зоне камеры B
		distAtoB := routing.HaversineMeters(camA.Lat, camA.Lon, camB.Lat, camB.Lon)
		if distAtoB < CameraRadiusMeters {
			log.Printf("⚠️ Камеры %d и %d слишком близко: %.0f м", camA.ID, camB.ID, distAtoB)
			skippedCount++
			continue
		}

		roadName := camA.RoadName
		if roadName == "" {
			roadName = camB.RoadName
		}
		if roadName == "" {
			roadName = "Неизвестная дорога"
		}

		log.Printf("📍 Камера A: (%.6f, %.6f) [ID=%d]", camA.Lat, camA.Lon, camA.ID)
		log.Printf("📍 Камера B: (%.6f, %.6f) [ID=%d]", camB.Lat, camB.Lon, camB.ID)
		log.Printf("📍 START: (%.6f, %.6f)", start.Lat, start.Lon)
		log.Printf("📍 END: (%.6f, %.6f) - вход в зону камеры B", end.Lat, end.Lon)
		log.Printf("✅ Маршрут %d -> %d, длина: %.2f км (чистый: %.2f км)",
			camA.ID, camB.ID, route.Distance/1000.0, cutDistance/1000.0)

		// Для чистого расстояния вычитаем 50 метров от камеры A
		clearStartPoint, _ := routing.PointAtDistance(cutGeometry, CameraRadiusMeters)
		clearDistance := cutDistance - CameraRadiusMeters

		if clearDistance < MinClearDistanceMeters {
			log.Printf("⚠️ Маршрут %d -> %d слишком короткий после вычета зоны A: %.0f м", camA.ID, camB.ID, clearDistance)
			skippedCount++
			continue
		}

		// Собираем обрезанную геометрию
		clearGeometry := f.buildClearGeometry(cutGeometry, CameraRadiusMeters, 0)

		result = append(result, Segment{
			StartLat:        clearStartPoint.Lat,
			StartLon:        clearStartPoint.Lon,
			EndLat:          end.Lat,
			EndLon:          end.Lon,
			DistanceKm:      route.Distance / 1000.0,
			ClearDistanceKm: clearDistance / 1000.0,
			DurationMin:     route.Duration / 60.0,
			RoadName:        roadName,
			StartCameraID:   camA.ID,
			EndCameraID:     camB.ID,
			Geometry:        clearGeometry,
		})

		if len(result) >= limit {
			break
		}
	}

	// 7. Сортируем по чистому расстоянию
	sort.Slice(result, func(i, j int) bool {
		return result[i].ClearDistanceKm > result[j].ClearDistanceKm
	})

	for i := range result {
		result[i].Rank = i + 1
		log.Printf("🏆 Маршрут #%d: %.2f км (чистый: %.2f км), %.0f мин (%s)",
			i+1, result[i].DistanceKm, result[i].ClearDistanceKm, result[i].DurationMin, result[i].RoadName)
	}

	log.Printf("⏱ Поиск занял %d мс, проверено %d пар, пропущено %d",
		time.Since(startTime).Milliseconds(), checkedCount, skippedCount)

	return result, nil
}

// findFirstIntermediateCamera находит первую промежуточную камеру на маршруте
func (f *RouteFinder) findFirstIntermediateCamera(
	cameras []repository.Camera,
	startIdx int,
	endIdx int,
	geometry []routing.Coordinate,
) (bool, int) {
	if len(geometry) < 2 {
		return false, -1
	}

	// Проверяем все камеры, кроме старта и конца
	for idx, camera := range cameras {
		if idx == startIdx || idx == endIdx {
			continue
		}

		// Проверяем расстояние от камеры до маршрута
		dist := routing.DistancePointToPolyline(camera.Lat, camera.Lon, geometry)
		if dist < IntermediateCameraRadius {
			log.Printf("  📷 Найдена промежуточная камера ID=%d (расстояние %.2f м)", camera.ID, dist)
			return true, idx
		}
	}

	return false, -1
}

// buildClearGeometry собирает обрезанную геометрию
func (f *RouteFinder) buildClearGeometry(geometry []routing.Coordinate, startOffset, endOffset float64) []routing.Coordinate {
	if len(geometry) < 2 {
		return geometry
	}

	// Находим точки на маршруте
	startPoint, _ := routing.PointAtDistance(geometry, startOffset)

	// Если endOffset > 0, обрезаем с конца
	var endPoint routing.Coordinate
	if endOffset > 0 {
		totalLen := routing.RouteLength(geometry)
		endPoint, _ = routing.PointAtDistance(geometry, totalLen-endOffset)
	} else {
		endPoint = geometry[len(geometry)-1]
	}

	// Собираем обрезанную геометрию
	result := []routing.Coordinate{startPoint}

	for i := 1; i < len(geometry)-1; i++ {
		// Проверяем, не вышли ли за пределы
		distFromStart := routing.HaversineMeters(startPoint.Lat, startPoint.Lon, geometry[i].Lat, geometry[i].Lon)
		distFromEnd := routing.HaversineMeters(endPoint.Lat, endPoint.Lon, geometry[i].Lat, geometry[i].Lon)

		// Если точка между startPoint и endPoint
		if distFromStart < routing.RouteLength(geometry) && distFromEnd < routing.RouteLength(geometry) {
			result = append(result, geometry[i])
		}
	}

	result = append(result, endPoint)

	return result
}
