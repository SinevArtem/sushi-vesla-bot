package service

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	"sushi-vesla-bot/internal/repository"
	"sushi-vesla-bot/internal/routing"
)

type Segment struct {
	StartLat      float64 `json:"start_lat"`
	StartLon      float64 `json:"start_lon"`
	EndLat        float64 `json:"end_lat"`
	EndLon        float64 `json:"end_lon"`
	DistanceKm    float64 `json:"distance_km"`
	DurationMin   float64 `json:"duration_min"`
	RoadName      string  `json:"road_name"`
	StartCameraID int     `json:"start_camera_id"`
	EndCameraID   int     `json:"end_camera_id"`
	Rank          int     `json:"rank"`
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

// GetRoute получает маршрут между двумя точками от OSRM
func (f *RouteFinder) GetRoute(ctx context.Context, startLat, startLon, endLat, endLon float64) (*routing.RouteResult, error) {
	return f.router.Route(ctx,
		routing.Coordinate{Lat: startLat, Lon: startLon},
		routing.Coordinate{Lat: endLat, Lon: endLon},
	)
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
		I        int
		J        int
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
				I:        i,
				J:        j,
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
		camA := cameras[candidate.I]
		camB := cameras[candidate.J]

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
		intermediateCameras := f.findIntermediateCameras(cameras, candidate.I, candidate.J, route.Geometry)

		if len(intermediateCameras) > 0 {
			log.Printf("❌ Маршрут %d -> %d содержит %d промежуточных камер: %v",
				camA.ID, camB.ID, len(intermediateCameras), intermediateCameras)
			skippedCount++
			continue
		}

		log.Printf("✅ Маршрут %d -> %d чист, длина: %.2f км",
			camA.ID, camB.ID, route.Distance/1000.0)

		// Добавляем в результат
		roadName := camA.RoadName
		if roadName == "" {
			roadName = camB.RoadName
		}
		if roadName == "" {
			roadName = "Неизвестная дорога"
		}

		result = append(result, Segment{
			StartLat:      camA.Lat,
			StartLon:      camA.Lon,
			EndLat:        camB.Lat,
			EndLon:        camB.Lon,
			DistanceKm:    route.Distance / 1000.0,
			DurationMin:   route.Duration / 60.0,
			RoadName:      roadName,
			StartCameraID: camA.ID,
			EndCameraID:   camB.ID,
		})

		if len(result) >= limit {
			break
		}
	}

	// 7. Присваиваем ранги
	for i := range result {
		result[i].Rank = i + 1
		log.Printf("🏆 Маршрут #%d: %.2f км, %.0f мин (%s)",
			i+1, result[i].DistanceKm, result[i].DurationMin, result[i].RoadName)
	}

	log.Printf("⏱ Поиск занял %d мс, проверено %d пар, пропущено %d (с промежуточными камерами)",
		time.Since(startTime).Milliseconds(), checkedCount, skippedCount)

	return result, nil
}

// findIntermediateCameras находит все промежуточные камеры на маршруте
func (f *RouteFinder) findIntermediateCameras(
	cameras []repository.Camera,
	startIdx int,
	endIdx int,
	geometry []routing.Coordinate,
) []int {
	var intermediateCameras []int

	if len(geometry) == 0 {
		return intermediateCameras
	}

	for idx, camera := range cameras {
		if idx == startIdx || idx == endIdx {
			continue
		}

		if pointNearRoute(camera.Lat, camera.Lon, geometry, 0.0005) {
			intermediateCameras = append(intermediateCameras, camera.ID)
		}
	}

	return intermediateCameras
}

// pointNearRoute проверяет, находится ли точка рядом с маршрутом
func pointNearRoute(lat, lon float64, geometry []routing.Coordinate, threshold float64) bool {
	if len(geometry) == 0 {
		return false
	}

	for _, p := range geometry {
		dist := haversineDistance(lat, lon, p.Lat, p.Lon)
		if dist < threshold {
			return true
		}
	}
	return false
}

// haversineDistance вычисляет расстояние между точками в км
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}
