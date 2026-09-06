package service

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"

	"sushi-vesla-bot/internal/repository"
)

type Segment struct {
	StartLat   float64 `json:"start_lat"`
	StartLon   float64 `json:"start_lon"`
	EndLat     float64 `json:"end_lat"`
	EndLon     float64 `json:"end_lon"`
	DistanceKm float64 `json:"distance_km"`
	RoadName   string  `json:"road_name"`
	Rank       int     `json:"rank"`
}

type RouteFinder struct {
	camRepo *repository.CameraRepository
}

func NewRouteFinder(camRepo *repository.CameraRepository) *RouteFinder {
	return &RouteFinder{camRepo: camRepo}
}

func (f *RouteFinder) FindLongestSegments(ctx context.Context, lat, lon float64, radiusMeters int, limit int) ([]Segment, error) {
	log.Printf("🔍 Поиск: lat=%.6f, lon=%.6f, radius=%d м", lat, lon, radiusMeters)

	// Получаем все камеры в радиусе
	cameras, err := f.camRepo.GetCamerasInRadius(ctx, lat, lon, radiusMeters)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения камер: %w", err)
	}

	log.Printf("📷 Найдено камер в радиусе %d м: %d", radiusMeters, len(cameras))

	if len(cameras) < 2 {
		return nil, fmt.Errorf("найдено только %d камер, нужно минимум 2", len(cameras))
	}

	// Находим все возможные участки между камерами
	var segments []Segment
	for i := 0; i < len(cameras); i++ {
		for j := i + 1; j < len(cameras); j++ {
			dist := haversineDistance(
				cameras[i].Lat, cameras[i].Lon,
				cameras[j].Lat, cameras[j].Lon,
			)

			// Минимальная длина участка - 500 метров
			if dist >= 0.5 {
				roadName := getRoadName(cameras[i], cameras[j])
				log.Printf("  🛣 Найден участок: %.2f км, %s", dist, roadName)

				segments = append(segments, Segment{
					StartLat:   cameras[i].Lat,
					StartLon:   cameras[i].Lon,
					EndLat:     cameras[j].Lat,
					EndLon:     cameras[j].Lon,
					DistanceKm: dist,
					RoadName:   roadName,
				})
			}
		}
	}

	log.Printf("📊 Всего найдено участков: %d", len(segments))

	if len(segments) == 0 {
		return nil, fmt.Errorf("не найдено участков длиннее 500 м")
	}

	// Сортируем по длине (от наибольшей к наименьшей)
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].DistanceKm > segments[j].DistanceKm
	})

	// Берем топ N
	if len(segments) > limit {
		segments = segments[:limit]
	}

	// Присваиваем ранги
	for i := range segments {
		segments[i].Rank = i + 1
		log.Printf("🏆 Маршрут #%d: %.2f км, %s", i+1, segments[i].DistanceKm, segments[i].RoadName)
	}

	return segments, nil
}

// haversineDistance вычисляет расстояние между точками в км
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371 // Радиус Земли в км

	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return R * c
}

func getRoadName(cam1, cam2 repository.Camera) string {
	if cam1.RoadName == cam2.RoadName && cam1.RoadName != "" {
		return cam1.RoadName
	}
	if cam1.RoadName != "" {
		return cam1.RoadName
	}
	if cam2.RoadName != "" {
		return cam2.RoadName
	}
	return "Неизвестная дорога"
}
