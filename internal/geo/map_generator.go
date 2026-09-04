package geo

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

type MapGenerator struct{}

func NewMapGenerator() *MapGenerator {
	return &MapGenerator{}
}

func (m *MapGenerator) GenerateHorizonMap(lat, lon, distanceKm float64) ([]byte, error) {
	// Создаём белый фон 600x400
	img := image.NewRGBA(image.Rect(0, 0, 600, 400))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	// Рисуем точку старта (красный круг)
	cx, cy := 100, 200
	for dy := -10; dy <= 10; dy++ {
		for dx := -10; dx <= 10; dx++ {
			if dx*dx+dy*dy <= 100 {
				img.Set(cx+dx, cy+dy, color.RGBA{255, 0, 0, 255})
			}
		}
	}

	// Рисуем линию к горизонту (зелёная стрела)
	endX := cx + int(distanceKm*30) // 1 км = 30 пикселей
	for x := cx; x < endX && x < 600; x++ {
		img.Set(x, cy, color.RGBA{0, 255, 0, 255})
	}

	// Рисуем флажок на конце
	if endX < 600 {
		img.Set(endX, cy, color.RGBA{255, 255, 0, 255})
		img.Set(endX+5, cy, color.RGBA{255, 255, 0, 255})
		img.Set(endX, cy-5, color.RGBA{255, 255, 0, 255})
		img.Set(endX, cy+5, color.RGBA{255, 255, 0, 255})
	}

	// Конвертируем в PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
