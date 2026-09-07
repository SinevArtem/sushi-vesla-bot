package service

import (
	"math"

	"sushi-vesla-bot/internal/routing"
)

const (
	EarthRadiusMeters = 6371000.0
)

type RouteProjection struct {
	DistanceAlong float64 // в метрах
	DistanceTo    float64 // в метрах
}

type RoutePoint struct {
	Point routing.Coordinate
	Index int
}

// haversineMeters вычисляет расстояние между точками в метрах
func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180

	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	return EarthRadiusMeters * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// routeLength вычисляет длину маршрута в метрах
func routeLength(geometry []routing.Coordinate) float64 {
	if len(geometry) < 2 {
		return 0
	}

	total := 0.0
	for i := 0; i < len(geometry)-1; i++ {
		total += haversineMeters(
			geometry[i].Lat, geometry[i].Lon,
			geometry[i+1].Lat, geometry[i+1].Lon,
		)
	}
	return total
}

// projectToSegment проецирует точку на отрезок
func projectToSegment(lat, lon float64, a, b routing.Coordinate) float64 {
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
		return 0
	}

	t := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)

	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}

// projectPointToRoute проецирует точку на маршрут
func projectPointToRoute(lat, lon float64, geometry []routing.Coordinate) (RouteProjection, bool) {
	if len(geometry) < 2 {
		return RouteProjection{}, false
	}

	totalDistance := 0.0
	bestDistance := math.MaxFloat64
	bestAlong := 0.0

	for i := 0; i < len(geometry)-1; i++ {
		a := geometry[i]
		b := geometry[i+1]

		segmentLength := haversineMeters(a.Lat, a.Lon, b.Lat, b.Lon)

		if segmentLength <= 0 {
			continue
		}

		t := projectToSegment(lat, lon, a, b)

		projLat := a.Lat + t*(b.Lat-a.Lat)
		projLon := a.Lon + t*(b.Lon-a.Lon)

		distanceTo := haversineMeters(lat, lon, projLat, projLon)
		along := totalDistance + t*segmentLength

		if distanceTo < bestDistance {
			bestDistance = distanceTo
			bestAlong = along
		}

		totalDistance += segmentLength
	}

	return RouteProjection{
		DistanceAlong: bestAlong,
		DistanceTo:    bestDistance,
	}, true
}

// pointAtDistance находит точку на маршруте на заданном расстоянии
func pointAtDistance(geometry []routing.Coordinate, target float64) RoutePoint {
	if len(geometry) == 0 {
		return RoutePoint{Point: routing.Coordinate{}, Index: 0}
	}

	if target <= 0 {
		return RoutePoint{Point: geometry[0], Index: 0}
	}

	accumulated := 0.0

	for i := 0; i < len(geometry)-1; i++ {
		a := geometry[i]
		b := geometry[i+1]

		segment := haversineMeters(a.Lat, a.Lon, b.Lat, b.Lon)

		if accumulated+segment >= target {
			remaining := target - accumulated
			t := remaining / segment

			return RoutePoint{
				Point: routing.Coordinate{
					Lat: a.Lat + t*(b.Lat-a.Lat),
					Lon: a.Lon + t*(b.Lon-a.Lon),
				},
				Index: i,
			}
		}

		accumulated += segment
	}

	last := len(geometry) - 1
	return RoutePoint{
		Point: geometry[last],
		Index: last,
	}
}

// trimRoute обрезает маршрут с обоих концов
func trimRoute(geometry []routing.Coordinate, startOffset, endOffset float64) ([]routing.Coordinate, float64, bool) {
	if len(geometry) < 2 {
		return nil, 0, false
	}

	total := routeLength(geometry)

	if total <= startOffset+endOffset {
		return nil, 0, false
	}

	start := pointAtDistance(geometry, startOffset)
	end := pointAtDistance(geometry, total-endOffset)

	if start.Index > end.Index {
		return nil, 0, false
	}

	// Собираем обрезанную геометрию
	result := []routing.Coordinate{start.Point}

	for i := start.Index + 1; i <= end.Index; i++ {
		result = append(result, geometry[i])
	}

	// Добавляем конечную точку, если её нет
	lastIdx := len(result) - 1
	if result[lastIdx].Lat != end.Point.Lat || result[lastIdx].Lon != end.Point.Lon {
		result = append(result, end.Point)
	}

	clearDistance := total - startOffset - endOffset

	return result, clearDistance, true
}
