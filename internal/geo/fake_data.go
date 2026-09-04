package geo

import (
	"math/rand"
	"time"
)

type DistanceService interface {
	GetMaxDistance(lat, lon float64) (float64, error)
}

// FakeDistanceService — временный мок
type FakeDistanceService struct{}

func NewFakeDistanceService() *FakeDistanceService {
	return &FakeDistanceService{}
}

func (f *FakeDistanceService) GetMaxDistance(lat, lon float64) (float64, error) {
	// ВРЕМЕННО: генерируем фейковые данные
	rand.Seed(time.Now().UnixNano())
	return 3.0 + rand.Float64()*15.0, nil // от 3 до 18 км
}
