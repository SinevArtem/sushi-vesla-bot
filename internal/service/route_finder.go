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

	// 2. Преобразуем камеры в точки для OSRM
	points := make([]routing.Coordinate, len(cameras))
	for i, camera := range cameras {
		points[i] = routing.Coordinate{
			Lat: camera.Lat,
			Lon: camera.Lon,
		}
	}

	// 3. Получаем матрицу дорожных расстояний через OSRM Table
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

	// 6. Проходим по кандидатам
	var result []Segment
	checkedCount := 0
	skippedCount := 0

	for _, candidate := range candidates {
		checkedCount++
		camA := cameras[candidate.A]
		camB := cameras[candidate.B]

		log.Printf("🔍 Проверяем пару ID=%d и ID=%d (расстояние: %.2f км)",
			camA.ID, camB.ID, candidate.Distance/1000.0)

		// Строим маршрут через OSRM Route
		route, err := f.router.Route(ctx,
			routing.Coordinate{Lat: camA.Lat, Lon: camA.Lon},
			routing.Coordinate{Lat: camB.Lat, Lon: camB.Lon},
		)
		if err != nil {
			log.Printf("⚠️ Ошибка маршрута %d -> %d: %v", camA.ID, camB.ID, err)
			continue
		}

		// Проверяем промежуточные камеры
		intermediateCameras, err := f.findIntermediateCameras(ctx, camA, camB, route.Geometry)
		if err != nil {
			log.Printf("⚠️ Ошибка проверки промежуточных камер: %v", err)
			continue
		}

		if len(intermediateCameras) > 0 {
			log.Printf("❌ Маршрут %d -> %d содержит %d промежуточных камер: %v",
				camA.ID, camB.ID, len(intermediateCameras), intermediateCameras)
			skippedCount++
			continue
		}

		// Обрезаем маршрут на 50 метров с каждого конца
		clearGeometry, clearDistance, ok := trimRoute(route.Geometry, CameraRadiusMeters, CameraRadiusMeters)
		if !ok {
			log.Printf("⚠️ Не удалось обрезать маршрут %d -> %d", camA.ID, camB.ID)
			skippedCount++
			continue
		}

		if clearDistance < MinClearDistanceMeters {
			log.Printf("⚠️ Маршрут %d -> %d слишком короткий после обрезки: %.0f м", camA.ID, camB.ID, clearDistance)
			skippedCount++
			continue
		}

		// Определяем начало и конец чистого участка
		start := clearGeometry[0]
		end := clearGeometry[len(clearGeometry)-1]

		roadName := camA.RoadName
		if roadName == "" {
			roadName = camB.RoadName
		}
		if roadName == "" {
			roadName = "Неизвестная дорога"
		}

		log.Printf("✅ Маршрут %d -> %d чист, длина: %.2f км (чистый участок: %.2f км)",
			camA.ID, camB.ID, route.Distance/1000.0, clearDistance/1000.0)

		result = append(result, Segment{
			StartLat:        start.Lat,
			StartLon:        start.Lon,
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

// findIntermediateCameras находит промежуточные камеры на маршруте
func (f *RouteFinder) findIntermediateCameras(
	ctx context.Context,
	start repository.Camera,
	end repository.Camera,
	geometry []routing.Coordinate,
) ([]int, error) {
	if len(geometry) < 2 {
		return nil, nil
	}

	// Преобразуем геометрию в формат для PostGIS
	geomCoords := make([][]float64, len(geometry))
	for i, p := range geometry {
		geomCoords[i] = []float64{p.Lon, p.Lat}
	}

	// Получаем камеры рядом с маршрутом
	cameras, err := f.camRepo.GetCamerasNearRoute(ctx, geomCoords, IntermediateCameraRadius)
	if err != nil {
		return nil, err
	}

	routeLengthMeters := routeLength(geometry)
	var intermediate []int

	for _, camera := range cameras {
		if camera.ID == start.ID || camera.ID == end.ID {
			continue
		}

		projection, ok := projectPointToRoute(camera.Lat, camera.Lon, geometry)
		if !ok {
			continue
		}

		if projection.DistanceTo > IntermediateCameraRadius {
			continue
		}

		// Камера должна быть внутри маршрута, а не рядом с концами
		if projection.DistanceAlong <= CameraRadiusMeters {
			continue
		}
		if projection.DistanceAlong >= routeLengthMeters-CameraRadiusMeters {
			continue
		}

		intermediate = append(intermediate, camera.ID)
	}

	return intermediate, nil
}
