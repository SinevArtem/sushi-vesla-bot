package routing

import (
	"math"
)

const (
	EarthRadiusMeters = 6371000.0
)

// HaversineMeters вычисляет расстояние между точками в метрах
func HaversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180

	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	return EarthRadiusMeters * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// RouteLength вычисляет длину маршрута в метрах
func RouteLength(geometry []Coordinate) float64 {
	if len(geometry) < 2 {
		return 0
	}

	total := 0.0
	for i := 0; i < len(geometry)-1; i++ {
		total += HaversineMeters(
			geometry[i].Lat, geometry[i].Lon,
			geometry[i+1].Lat, geometry[i+1].Lon,
		)
	}
	return total
}

// DistancePointToSegment вычисляет расстояние от точки до отрезка в метрах
func DistancePointToSegment(lat, lon float64, a, b Coordinate) float64 {
	// Используем локальную проекцию в метрах
	lat0 := (a.Lat + b.Lat + lat) / 3

	kx := 111320.0 * math.Cos(lat0*math.Pi/180)
	ky := 110540.0

	ax := a.Lon * kx
	ay := a.Lat * ky

	bx := b.Lon * kx
	by := b.Lat * ky

	px := lon * kx
	py := lat * ky

	dx := bx - ax
	dy := by - ay

	if dx == 0 && dy == 0 {
		return HaversineMeters(lat, lon, a.Lat, a.Lon)
	}

	t := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)

	if t < 0 {
		return HaversineMeters(lat, lon, a.Lat, a.Lon)
	}
	if t > 1 {
		return HaversineMeters(lat, lon, b.Lat, b.Lon)
	}

	projLat := a.Lat + t*(b.Lat-a.Lat)
	projLon := a.Lon + t*(b.Lon-a.Lon)

	return HaversineMeters(lat, lon, projLat, projLon)
}

// DistancePointToPolyline вычисляет минимальное расстояние от точки до полилинии
func DistancePointToPolyline(lat, lon float64, geometry []Coordinate) float64 {
	if len(geometry) < 2 {
		if len(geometry) == 1 {
			return HaversineMeters(lat, lon, geometry[0].Lat, geometry[0].Lon)
		}
		return math.MaxFloat64
	}

	minDist := math.MaxFloat64
	for i := 0; i < len(geometry)-1; i++ {
		dist := DistancePointToSegment(lat, lon, geometry[i], geometry[i+1])
		if dist < minDist {
			minDist = dist
		}
	}
	return minDist
}

// PointAtDistance находит точку на маршруте на заданном расстоянии
func PointAtDistance(geometry []Coordinate, target float64) (Coordinate, int) {
	if len(geometry) == 0 {
		return Coordinate{}, 0
	}

	if target <= 0 {
		return geometry[0], 0
	}

	accumulated := 0.0
	for i := 0; i < len(geometry)-1; i++ {
		a := geometry[i]
		b := geometry[i+1]

		segment := HaversineMeters(a.Lat, a.Lon, b.Lat, b.Lon)

		if accumulated+segment >= target {
			remaining := target - accumulated
			t := remaining / segment

			return Coordinate{
				Lat: a.Lat + t*(b.Lat-a.Lat),
				Lon: a.Lon + t*(b.Lon-a.Lon),
			}, i
		}

		accumulated += segment
	}

	return geometry[len(geometry)-1], len(geometry) - 1
}

// CutRouteAtCircle обрезает маршрут при входе в окружность
func CutRouteAtCircle(
	geometry []Coordinate,
	centerLat, centerLon float64,
	radiusMeters float64,
) ([]Coordinate, float64, bool) {
	if len(geometry) < 2 {
		return nil, 0, false
	}

	var result []Coordinate
	totalLength := 0.0
	foundIntersection := false

	for i := 0; i < len(geometry)-1; i++ {
		a := geometry[i]
		b := geometry[i+1]

		distA := HaversineMeters(a.Lat, a.Lon, centerLat, centerLon)
		distB := HaversineMeters(b.Lat, b.Lon, centerLat, centerLon)

		// Если начало сегмента уже внутри круга
		if distA <= radiusMeters {
			// Добавляем точку входа (пересечение с окружностью)
			entryPoint, found := findCircleIntersection(a, b, centerLat, centerLon, radiusMeters)
			if found {
				result = append(result, entryPoint)
				totalLength += HaversineMeters(a.Lat, a.Lon, entryPoint.Lat, entryPoint.Lon)
				foundIntersection = true
				break
			}
			// Если не нашли пересечение, добавляем начало
			if len(result) == 0 {
				result = append(result, a)
			}
			break
		}

		// Если сегмент пересекает окружность
		if distA > radiusMeters && distB <= radiusMeters {
			entryPoint, found := findCircleIntersection(a, b, centerLat, centerLon, radiusMeters)
			if found {
				// Добавляем все предыдущие точки
				for j := 0; j <= i; j++ {
					result = append(result, geometry[j])
				}
				// Добавляем точку пересечения
				result = append(result, entryPoint)
				totalLength += HaversineMeters(a.Lat, a.Lon, entryPoint.Lat, entryPoint.Lon)
				foundIntersection = true
				break
			}
		}

		// Добавляем точку a в результат
		if len(result) == 0 || i == 0 {
			result = append(result, a)
		}

		segLen := HaversineMeters(a.Lat, a.Lon, b.Lat, b.Lon)
		totalLength += segLen
	}

	// Если нашли пересечение, возвращаем результат
	if foundIntersection && len(result) > 0 {
		return result, totalLength, true
	}

	// Если не нашли пересечение, возвращаем весь маршрут
	return geometry, RouteLength(geometry), true
}

// findCircleIntersection находит пересечение отрезка с окружностью (бинарный поиск)
func findCircleIntersection(a, b Coordinate, centerLat, centerLon, radius float64) (Coordinate, bool) {
	// Бинарный поиск для нахождения точки пересечения
	left := a
	right := b

	// Проверяем, что одна точка внутри, другая снаружи
	distLeft := HaversineMeters(left.Lat, left.Lon, centerLat, centerLon)
	distRight := HaversineMeters(right.Lat, right.Lon, centerLat, centerLon)

	// Если обе внутри или обе снаружи, возвращаем середину
	if (distLeft <= radius && distRight <= radius) || (distLeft > radius && distRight > radius) {
		// Возвращаем точку, которая ближе к окружности
		if math.Abs(distLeft-radius) < math.Abs(distRight-radius) {
			return left, true
		}
		return right, true
	}

	// Бинарный поиск (30 итераций для точности ~1 метр)
	for i := 0; i < 30; i++ {
		mid := Coordinate{
			Lat: (left.Lat + right.Lat) / 2,
			Lon: (left.Lon + right.Lon) / 2,
		}
		distMid := HaversineMeters(mid.Lat, mid.Lon, centerLat, centerLon)

		if distMid < radius {
			right = mid
		} else {
			left = mid
		}
	}

	// Возвращаем точку на границе
	return right, true
}
