package geo

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font/gofont/goregular"
)

type MapGenerator struct{}

func NewMapGenerator() *MapGenerator {
	return &MapGenerator{}
}

func (m *MapGenerator) GenerateHorizonMap(lat, lon, distanceKm float64) ([]byte, error) {
	// Создаём изображение 800x500
	img := image.NewRGBA(image.Rect(0, 0, 800, 500))

	// Заливаем тёмный фон (ночной режим)
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{20, 20, 30, 255}}, image.Point{}, draw.Src)

	// Рисуем сетку
	for x := 0; x < 800; x += 40 {
		for y := 0; y < 500; y += 40 {
			img.Set(x, y, color.RGBA{40, 40, 50, 255})
		}
	}

	// Центр карты
	cx, cy := 200, 250

	// Масштаб: 1 км = 20 пикселей, но с ограничением
	scale := math.Min(20.0, 800.0/(distanceKm*2+1))
	if scale < 1 {
		scale = 1
	}

	pixels := int(distanceKm * scale)
	maxPixels := 500
	if pixels > maxPixels {
		pixels = maxPixels
	}

	// Рисуем точку старта (красный круг с эффектом свечения)
	for r := 0; r < 20; r++ {
		alpha := 255 - r*12
		if alpha < 0 {
			alpha = 0
		}
		alphaUint8 := uint8(alpha)
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if dx*dx+dy*dy <= r*r {
					if r < 5 {
						img.Set(cx+dx, cy+dy, color.RGBA{255, 50, 50, alphaUint8})
					} else {
						img.Set(cx+dx, cy+dy, color.RGBA{255, 0, 0, uint8(alpha / 2)})
					}
				}
			}
		}
	}

	// Рисуем линию к горизонту (светящаяся зелёная)
	for i := 0; i < pixels; i++ {
		x := cx + i
		if x >= 800 {
			break
		}
		// Основная линия
		for dy := -3; dy <= 3; dy++ {
			alpha := uint8(255 - abs(dy)*50)
			img.Set(x, cy+dy, color.RGBA{0, 255, 100, alpha})
		}
		// Свечение
		for dy := -6; dy <= 6; dy++ {
			if abs(dy) > 3 {
				alpha := uint8(100 - abs(dy)*15)
				img.Set(x, cy+dy, color.RGBA{0, 255, 100, alpha})
			}
		}
	}

	// Рисуем финишный флажок
	endX := cx + pixels
	if endX < 800 {
		// Флаг
		for dy := -15; dy <= 15; dy++ {
			if dy >= -15 && dy <= 5 {
				img.Set(endX+10, cy+dy, color.RGBA{255, 255, 0, 255})
			}
			img.Set(endX, cy+dy, color.RGBA{255, 255, 255, 255})
		}
		// Свечение вокруг флага
		for r := 0; r < 20; r++ {
			alpha := 100 - r*5
			if alpha < 0 {
				alpha = 0
			}
			for dy := -r; dy <= r; dy++ {
				for dx := -r; dx <= r; dx++ {
					if dx*dx+dy*dy <= r*r && r > 15 {
						img.Set(endX+dx, cy+dy, color.RGBA{255, 255, 0, uint8(alpha / 2)})
					}
				}
			}
		}
	}

	// Загружаем шрифт для текста
	font, err := truetype.Parse(goregular.TTF)
	if err != nil {
		return nil, fmt.Errorf("ошибка загрузки шрифта: %w", err)
	}

	// Создаём контекст для рисования текста
	c := freetype.NewContext()
	c.SetDPI(72)
	c.SetFont(font)
	c.SetFontSize(14)
	c.SetClip(img.Bounds())
	c.SetDst(img)
	c.SetSrc(image.NewUniform(color.RGBA{255, 100, 100, 255}))

	// Рисуем текст "СТАРТ"
	pt := freetype.Pt(10, 30)
	if _, err := c.DrawString("📍 СТАРТ", pt); err != nil {
		return nil, err
	}

	// Рисуем текст "ГОРИЗОНТ"
	c.SetSrc(image.NewUniform(color.RGBA{100, 255, 100, 255}))
	pt = freetype.Pt(endX-40, 30)
	if _, err := c.DrawString("🏁 ГОРИЗОНТ", pt); err != nil {
		return nil, err
	}

	// Рисуем расстояние
	c.SetFontSize(12)
	c.SetSrc(image.NewUniform(color.RGBA{200, 200, 200, 255}))
	distanceText := fmt.Sprintf("📏 Расстояние: %.1f км", distanceKm)
	pt = freetype.Pt(10, 470)
	if _, err := c.DrawString(distanceText, pt); err != nil {
		return nil, err
	}

	// Рисуем координаты
	coordText := formatCoords(lat, lon)
	pt = freetype.Pt(10, 490)
	if _, err := c.DrawString(coordText, pt); err != nil {
		return nil, err
	}

	// Конвертируем в PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func formatCoords(lat, lon float64) string {
	latDir := "N"
	if lat < 0 {
		latDir = "S"
	}
	lonDir := "E"
	if lon < 0 {
		lonDir = "W"
	}
	return fmt.Sprintf("📍 %.4f°%s, %.4f°%s", math.Abs(lat), latDir, math.Abs(lon), lonDir)
}
