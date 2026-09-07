package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Camera struct {
	ID         int     `json:"id"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	SpeedLimit int     `json:"speed_limit"`
	RoadName   string  `json:"road_name"`
}

type CameraRepository struct {
	pool *pgxpool.Pool
}

func NewCameraRepository(dbURL string) (*CameraRepository, error) {
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		return nil, fmt.Errorf("ошибка подключения к БД: %w", err)
	}
	return &CameraRepository{pool: pool}, nil
}

func (r *CameraRepository) Close() {
	r.pool.Close()
}

func (r *CameraRepository) GetCamerasInRadius(ctx context.Context, lat, lon float64, radiusMeters int) ([]Camera, error) {
	query := `
		SELECT 
			id, lat, lon, speed_limit, road_name
		FROM cameras
		WHERE ST_DWithin(
			geom::geography,
			ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
			$3
		)
		ORDER BY id
	`

	rows, err := r.pool.Query(ctx, query, lon, lat, radiusMeters)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer rows.Close()

	var cameras []Camera
	for rows.Next() {
		var c Camera
		err := rows.Scan(&c.ID, &c.Lat, &c.Lon, &c.SpeedLimit, &c.RoadName)
		if err != nil {
			return nil, fmt.Errorf("ошибка сканирования: %w", err)
		}
		cameras = append(cameras, c)
	}

	return cameras, nil
}

func (r *CameraRepository) GetAllCameras(ctx context.Context) ([]Camera, error) {
	query := `
		SELECT 
			id, lat, lon, speed_limit, road_name
		FROM cameras
		ORDER BY id
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cameras []Camera
	for rows.Next() {
		var c Camera
		err := rows.Scan(&c.ID, &c.Lat, &c.Lon, &c.SpeedLimit, &c.RoadName)
		if err != nil {
			return nil, err
		}
		cameras = append(cameras, c)
	}

	return cameras, nil
}

// GetCamerasNearRoute находит камеры рядом с маршрутом
func (r *CameraRepository) GetCamerasNearRoute(ctx context.Context, geometry [][]float64, radiusMeters float64) ([]Camera, error) {
	if len(geometry) < 2 {
		return nil, fmt.Errorf("геометрия маршрута слишком короткая")
	}

	var wkt strings.Builder
	wkt.WriteString("LINESTRING(")

	for i, p := range geometry {
		if i > 0 {
			wkt.WriteString(",")
		}
		wkt.WriteString(fmt.Sprintf("%.7f %.7f", p[0], p[1])) // lon, lat
	}

	wkt.WriteString(")")

	query := `
		SELECT
			id,
			lat,
			lon,
			speed_limit,
			road_name
		FROM cameras
		WHERE ST_DWithin(
			geom::geography,
			ST_SetSRID(
				ST_GeomFromText($1),
				4326
			)::geography,
			$2
		)
		ORDER BY id
	`

	rows, err := r.pool.Query(ctx, query, wkt.String(), radiusMeters)
	if err != nil {
		return nil, fmt.Errorf("ошибка поиска камер около маршрута: %w", err)
	}
	defer rows.Close()

	var cameras []Camera
	for rows.Next() {
		var c Camera
		if err := rows.Scan(&c.ID, &c.Lat, &c.Lon, &c.SpeedLimit, &c.RoadName); err != nil {
			return nil, fmt.Errorf("ошибка чтения камеры: %w", err)
		}
		cameras = append(cameras, c)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return cameras, nil
}

func (r *CameraRepository) GetCamerasByCoordinates(ctx context.Context, lat, lon float64) ([]Camera, error) {
	query := `
		SELECT 
			id, lat, lon, speed_limit, road_name
		FROM cameras
		WHERE ST_DWithin(
			geom::geography,
			ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
			50
		)
		ORDER BY id
	`

	rows, err := r.pool.Query(ctx, query, lon, lat)
	if err != nil {
		return nil, fmt.Errorf("ошибка поиска камеры: %w", err)
	}
	defer rows.Close()

	var cameras []Camera
	for rows.Next() {
		var c Camera
		err := rows.Scan(&c.ID, &c.Lat, &c.Lon, &c.SpeedLimit, &c.RoadName)
		if err != nil {
			return nil, fmt.Errorf("ошибка сканирования: %w", err)
		}
		cameras = append(cameras, c)
	}

	return cameras, nil
}

func (r *CameraRepository) AddCamera(ctx context.Context, camera Camera) (int, error) {
	query := `
		INSERT INTO cameras (lat, lon, geom, speed_limit, road_name)
		VALUES ($1, $2, ST_SetSRID(ST_MakePoint($2, $1), 4326), $3, $4)
		RETURNING id
	`

	var id int
	err := r.pool.QueryRow(ctx, query, camera.Lat, camera.Lon, camera.SpeedLimit, camera.RoadName).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ошибка добавления камеры: %w", err)
	}

	return id, nil
}

func (r *CameraRepository) DeleteCamera(ctx context.Context, id int) error {
	query := `DELETE FROM cameras WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("ошибка удаления камеры: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("камера с ID %d не найдена", id)
	}

	return nil
}
