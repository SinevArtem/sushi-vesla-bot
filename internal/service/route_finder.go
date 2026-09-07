package service

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"sushi-vesla-bot/internal/repository"
	"sushi-vesla-bot/internal/routing"
)

const (
	CameraRadiusMeters       = 80.0
	IntermediateCameraRadius = 100.0
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

// FindLongestSegmentsParallel - параллельный поиск маршрутов
func (f *RouteFinder) FindLongestSegmentsParallel(ctx context.Context, lat, lon float64, radiusMeters int, limit int) ([]Segment, error) {
	startTime := time.Now()
	log.Printf("🔍 Параллельный поиск: lat=%.6f, lon=%.6f, radius=%d м", lat, lon, radiusMeters)

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

	// 2. Для каждой камеры получаем привязку к дороге через OSRM Nearest (параллельно)
	roadPoints := make([]routing.Coordinate, len(cameras))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, camera := range cameras {
		wg.Add(1)
		go func(idx int, cam repository.Camera) {
			defer wg.Done()
			nearest, err := f.router.Nearest(ctx, cam.Lat, cam.Lon)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				log.Printf("⚠️ Ошибка Nearest для камеры ID=%d: %v", cam.ID, err)
				roadPoints[idx] = routing.Coordinate{Lat: cam.Lat, Lon: cam.Lon}
				return
			}
			log.Printf("  Камера ID=%d привязана к дороге: (%.6f, %.6f), расстояние %.2f м",
				cam.ID, nearest.Coordinate.Lat, nearest.Coordinate.Lon, nearest.Distance)
			roadPoints[idx] = nearest.Coordinate
		}(i, camera)
	}
	wg.Wait()

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
		Distance float64
		Duration float64
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

	// Сортируем кандидатов по убыванию расстояния (от самого длинного)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Distance > candidates[j].Distance
	})

	log.Printf("📊 Найдено %d пар камер", len(candidates))

	// 5. Параллельная обработка кандидатов
	type Result struct {
		Segment Segment
		Error   error
		Index   int
	}

	var resultMu sync.Mutex
	var results []Segment
	var wgProcess sync.WaitGroup
	semaphore := make(chan struct{}, 5)

	skippedCount := 0
	var countMu sync.Mutex

	for idx, candidate := range candidates {
		if len(results) >= limit*2 {
			break
		}

		wgProcess.Add(1)
		go func(candidateIdx int, cand Candidate) {
			defer wgProcess.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			camA := cameras[cand.A]
			camB := cameras[cand.B]
			roadA := roadPoints[cand.A]
			roadB := roadPoints[cand.B]

			log.Printf("🔍 Проверяем пару ID=%d и ID=%d (расстояние: %.2f км)",
				camA.ID, camB.ID, cand.Distance/1000.0)

			route, err := f.router.Route(ctx, roadA, roadB)
			if err != nil {
				log.Printf("⚠️ Ошибка маршрута %d -> %d: %v", camA.ID, camB.ID, err)
				countMu.Lock()
				skippedCount++
				countMu.Unlock()
				return
			}

			// Проверяем промежуточные камеры
			hasIntermediate, interIdx := f.findFirstIntermediateCamera(cameras, cand.A, cand.B, route.Geometry)
			if hasIntermediate {
				log.Printf("❌ Маршрут %d -> %d содержит промежуточную камеру ID=%d", camA.ID, camB.ID, cameras[interIdx].ID)
				countMu.Lock()
				skippedCount++
				countMu.Unlock()
				return
			}

			// Обрезаем маршрут при входе в зону камеры B
			cutGeometry, cutDistance, ok := routing.CutRouteAtCircle(
				route.Geometry,
				camB.Lat, camB.Lon,
				CameraRadiusMeters,
			)

			if !ok || cutDistance < MinClearDistanceMeters {
				countMu.Lock()
				skippedCount++
				countMu.Unlock()
				return
			}

			// Проверяем петлю
			if f.routeReturnsToStart(camA, cutGeometry) {
				log.Printf("⚠️ Маршрут %d -> %d возвращается к камере %d, пропускаем", camA.ID, camB.ID, camA.ID)
				countMu.Lock()
				skippedCount++
				countMu.Unlock()
				return
			}

			end := cutGeometry[len(cutGeometry)-1]

			distAtoB := routing.HaversineMeters(camA.Lat, camA.Lon, camB.Lat, camB.Lon)
			if distAtoB < CameraRadiusMeters {
				countMu.Lock()
				skippedCount++
				countMu.Unlock()
				return
			}

			roadName := camA.RoadName
			if roadName == "" {
				roadName = camB.RoadName
			}
			if roadName == "" {
				roadName = "Неизвестная дорога"
			}

			// Чистое расстояние = расстояние до входа в зону B минус 80 метров зоны A
			clearDistance := cutDistance - CameraRadiusMeters
			if clearDistance < MinClearDistanceMeters {
				countMu.Lock()
				skippedCount++
				countMu.Unlock()
				return
			}

			// Находим точку на 80 метров от начала (край зоны A)
			clearStartPoint, _ := routing.PointAtDistance(cutGeometry, CameraRadiusMeters)
			clearGeometry := f.buildClearGeometry(cutGeometry, CameraRadiusMeters, 0)

			resultMu.Lock()
			results = append(results, Segment{
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
			resultMu.Unlock()

			log.Printf("✅ Маршрут %d -> %d добавлен, длина: %.2f км", camA.ID, camB.ID, clearDistance/1000.0)
		}(idx, candidate)
	}

	wgProcess.Wait()

	// 6. Сортируем по чистому расстоянию (от самого длинного к самому короткому)
	sort.Slice(results, func(i, j int) bool {
		return results[i].ClearDistanceKm > results[j].ClearDistanceKm
	})

	// 7. Ограничиваем количество результатов
	if len(results) > limit {
		results = results[:limit]
	}

	// 8. Присваиваем ранги
	for i := range results {
		results[i].Rank = i + 1
		log.Printf("🏆 Маршрут #%d: %.2f км (чистый: %.2f км), %.0f мин (%s)",
			i+1, results[i].DistanceKm, results[i].ClearDistanceKm, results[i].DurationMin, results[i].RoadName)
	}

	log.Printf("⏱ Параллельный поиск занял %d мс, обработано %d пар, пропущено %d",
		time.Since(startTime).Milliseconds(), len(candidates), skippedCount)

	return results, nil
}

// FindLongestSegments - синхронная версия (для совместимости)
func (f *RouteFinder) FindLongestSegments(ctx context.Context, lat, lon float64, radiusMeters int, limit int) ([]Segment, error) {
	return f.FindLongestSegmentsParallel(ctx, lat, lon, radiusMeters, limit)
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

	for idx, camera := range cameras {
		if idx == startIdx || idx == endIdx {
			continue
		}

		dist := routing.DistancePointToPolyline(camera.Lat, camera.Lon, geometry)
		if dist < IntermediateCameraRadius {
			log.Printf("  📷 Найдена промежуточная камера ID=%d (расстояние %.2f м)", camera.ID, dist)
			return true, idx
		}
	}

	return false, -1
}

// routeReturnsToStart проверяет, не возвращается ли маршрут к камере A
func (f *RouteFinder) routeReturnsToStart(cam repository.Camera, geometry []routing.Coordinate) bool {
	if len(geometry) < 5 {
		return false
	}

	totalLen := routing.RouteLength(geometry)
	checkStart := 80.0
	checkEnd := totalLen - 80.0

	if checkEnd <= checkStart {
		return false
	}

	step := 50.0
	for dist := checkStart; dist < checkEnd; dist += step {
		point, _ := routing.PointAtDistance(geometry, dist)
		distToCamera := routing.HaversineMeters(point.Lat, point.Lon, cam.Lat, cam.Lon)

		if distToCamera < CameraRadiusMeters {
			return true
		}
	}

	return false
}

// buildClearGeometry собирает обрезанную геометрию
func (f *RouteFinder) buildClearGeometry(geometry []routing.Coordinate, startOffset, endOffset float64) []routing.Coordinate {
	if len(geometry) < 2 {
		return geometry
	}

	startPoint, _ := routing.PointAtDistance(geometry, startOffset)

	var endPoint routing.Coordinate
	if endOffset > 0 {
		totalLen := routing.RouteLength(geometry)
		endPoint, _ = routing.PointAtDistance(geometry, totalLen-endOffset)
	} else {
		endPoint = geometry[len(geometry)-1]
	}

	result := []routing.Coordinate{startPoint}

	for i := 1; i < len(geometry)-1; i++ {
		distFromStart := routing.HaversineMeters(startPoint.Lat, startPoint.Lon, geometry[i].Lat, geometry[i].Lon)
		distFromEnd := routing.HaversineMeters(endPoint.Lat, endPoint.Lon, geometry[i].Lat, geometry[i].Lon)

		if distFromStart < routing.RouteLength(geometry) && distFromEnd < routing.RouteLength(geometry) {
			result = append(result, geometry[i])
		}
	}

	result = append(result, endPoint)

	return result
}
