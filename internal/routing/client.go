package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Coordinate struct {
	Lat float64
	Lon float64
}

type MatrixResult struct {
	Distance float64 // в метрах
	Duration float64 // в секундах
}

type RouteResult struct {
	Distance float64 // в метрах
	Duration float64 // в секундах
	Geometry []Coordinate
}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Table получает дорожные расстояния между всеми точками
func (c *Client) Table(ctx context.Context, points []Coordinate) ([][]MatrixResult, error) {
	if len(points) < 2 {
		return nil, fmt.Errorf("нужно минимум 2 точки")
	}

	var coords strings.Builder
	for i, p := range points {
		if i > 0 {
			coords.WriteString(";")
		}
		coords.WriteString(strconv.FormatFloat(p.Lon, 'f', 6, 64))
		coords.WriteString(",")
		coords.WriteString(strconv.FormatFloat(p.Lat, 'f', 6, 64))
	}

	url := fmt.Sprintf("%s/table/v1/driving/%s?annotations=distance,duration", c.baseURL, coords.String())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("создание table запроса: %w", err)
	}
	req.Header.Set("User-Agent", "SushiVeslaBot/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("table request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OSRM table HTTP %d", resp.StatusCode)
	}

	var result struct {
		Code      string      `json:"code"`
		Distances [][]float64 `json:"distances"`
		Durations [][]float64 `json:"durations"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode table response: %w", err)
	}

	if result.Code != "Ok" {
		return nil, fmt.Errorf("OSRM table code: %s", result.Code)
	}

	matrix := make([][]MatrixResult, len(result.Distances))
	for i := range result.Distances {
		matrix[i] = make([]MatrixResult, len(result.Distances[i]))
		for j := range result.Distances[i] {
			matrix[i][j] = MatrixResult{
				Distance: result.Distances[i][j],
				Duration: result.Durations[i][j],
			}
		}
	}

	return matrix, nil
}

// Route строит автомобильный маршрут
func (c *Client) Route(ctx context.Context, start, end Coordinate) (*RouteResult, error) {
	url := fmt.Sprintf(
		"%s/route/v1/driving/%.6f,%.6f;%.6f,%.6f?overview=full&geometries=geojson",
		c.baseURL, start.Lon, start.Lat, end.Lon, end.Lat,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("создание route запроса: %w", err)
	}
	req.Header.Set("User-Agent", "SushiVeslaBot/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("route request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OSRM route HTTP %d", resp.StatusCode)
	}

	var result struct {
		Code   string `json:"code"`
		Routes []struct {
			Distance float64 `json:"distance"`
			Duration float64 `json:"duration"`
			Geometry struct {
				Type        string      `json:"type"`
				Coordinates [][]float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"routes"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode route response: %w", err)
	}

	if result.Code != "Ok" {
		return nil, fmt.Errorf("OSRM route code: %s", result.Code)
	}

	if len(result.Routes) == 0 {
		return nil, fmt.Errorf("OSRM не вернул маршрут")
	}

	r := result.Routes[0]

	geometry := make([]Coordinate, 0, len(r.Geometry.Coordinates))
	for _, p := range r.Geometry.Coordinates {
		if len(p) != 2 {
			continue
		}
		geometry = append(geometry, Coordinate{
			Lat: p[1],
			Lon: p[0],
		})
	}

	return &RouteResult{
		Distance: r.Distance,
		Duration: r.Duration,
		Geometry: geometry,
	}, nil
}
