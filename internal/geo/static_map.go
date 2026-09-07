package geo

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font/gofont/goregular"

	"sushi-vesla-bot/internal/service"
)

type StaticMapGenerator struct {
	httpClient *http.Client
}

func NewStaticMapGenerator() *StaticMapGenerator {
	return &StaticMapGenerator{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// GenerateRouteImage генерирует изображение маршрута из сегмента
func (g *StaticMapGenerator) GenerateRouteImage(segment service.Segment) ([]byte, error) {
	// Пробуем получить реальную карту из OSM
	if imgBytes, err := g.generateOSMMap(segment); err == nil {
		return imgBytes, nil
	}

	// Если OSM не работает - рисуем схематичную карту
	return g.generateSchematicMap(segment)
}

// generateOSMMap - получает реальную карту из OpenStreetMap
func (g *StaticMapGenerator) generateOSMMap(segment service.Segment) ([]byte, error) {
	geometry := segment.Geometry
	if len(geometry) < 2 {
		return nil, fmt.Errorf("геометрия маршрута слишком короткая")
	}

	// Находим границы
	minLat, maxLat := geometry[0].Lat, geometry[0].Lat
	minLon, maxLon := geometry[0].Lon, geometry[0].Lon

	for _, p := range geometry {
		if p.Lat < minLat {
			minLat = p.Lat
		}
		if p.Lat > maxLat {
			maxLat = p.Lat
		}
		if p.Lon < minLon {
			minLon = p.Lon
		}
		if p.Lon > maxLon {
			maxLon = p.Lon
		}
	}

	// Добавляем отступы
	padding := 0.02
	minLat -= padding
	maxLat += padding
	minLon -= padding
	maxLon += padding

	centerLat := (minLat + maxLat) / 2
	centerLon := (minLon + maxLon) / 2

	// Вычисляем zoom
	latDiff := maxLat - minLat
	lonDiff := maxLon - minLon
	zoom := 13
	if latDiff > 0.1 || lonDiff > 0.1 {
		zoom = 11
	} else if latDiff > 0.05 || lonDiff > 0.05 {
		zoom = 12
	} else if latDiff > 0.02 || lonDiff > 0.02 {
		zoom = 13
	} else {
		zoom = 14
	}

	// Формируем URL для статической карты OSM
	url := fmt.Sprintf(
		"https://staticmap.openstreetmap.de/staticmap.php?center=%.6f,%.6f&zoom=%d&size=800x600&maptype=mapnik",
		centerLat, centerLon, zoom,
	)

	// Добавляем маркеры
	url += fmt.Sprintf("&markers=%d|%.6f,%.6f|red-dot", 0, segment.StartLat, segment.StartLon)
	url += fmt.Sprintf("&markers=%d|%.6f,%.6f|blue-dot", 0, segment.EndLat, segment.EndLon)

	// Добавляем линию маршрута
	if len(geometry) > 1 {
		pathStr := ""
		for i, p := range geometry {
			if i > 0 {
				pathStr += "|"
			}
			pathStr += fmt.Sprintf("%.6f,%.6f", p.Lat, p.Lon)
		}
		url += fmt.Sprintf("&path=0|FF0000|%s", pathStr)
	}

	resp, err := g.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OSM static map error: %d", resp.StatusCode)
	}

	imgBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return imgBytes, nil
}

// generateSchematicMap - схематичная карта (запасной вариант)
func (g *StaticMapGenerator) generateSchematicMap(segment service.Segment) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{20, 20, 30, 255}}, image.Point{}, draw.Src)

	for x := 0; x < 800; x += 40 {
		for y := 0; y < 600; y += 40 {
			img.Set(x, y, color.RGBA{40, 40, 50, 255})
		}
	}

	geometry := segment.Geometry
	if len(geometry) < 2 {
		return g.drawSimpleRoute(img, segment)
	}

	return g.drawRouteWithGeometry(img, segment)
}

func (g *StaticMapGenerator) drawRouteWithGeometry(img *image.RGBA, segment service.Segment) ([]byte, error) {
	geometry := segment.Geometry

	minLat, maxLat := geometry[0].Lat, geometry[0].Lat
	minLon, maxLon := geometry[0].Lon, geometry[0].Lon

	for _, p := range geometry {
		if p.Lat < minLat {
			minLat = p.Lat
		}
		if p.Lat > maxLat {
			maxLat = p.Lat
		}
		if p.Lon < minLon {
			minLon = p.Lon
		}
		if p.Lon > maxLon {
			maxLon = p.Lon
		}
	}

	paddingLat := (maxLat - minLat) * 0.1
	paddingLon := (maxLon - minLon) * 0.1
	if paddingLat < 0.001 {
		paddingLat = 0.001
	}
	if paddingLon < 0.001 {
		paddingLon = 0.001
	}

	minLat -= paddingLat
	maxLat += paddingLat
	minLon -= paddingLon
	maxLon += paddingLon

	toPixel := func(lat, lon float64) (int, int) {
		x := int((lon-minLon)/(maxLon-minLon)*780) + 10
		y := int((1-(lat-minLat)/(maxLat-minLat))*580) + 10
		if x < 0 {
			x = 0
		}
		if x > 799 {
			x = 799
		}
		if y < 0 {
			y = 0
		}
		if y > 599 {
			y = 599
		}
		return x, y
	}

	for i := 0; i < len(geometry)-1; i++ {
		x1, y1 := toPixel(geometry[i].Lat, geometry[i].Lon)
		x2, y2 := toPixel(geometry[i+1].Lat, geometry[i+1].Lon)

		for thickness := 0; thickness < 4; thickness++ {
			alpha := uint8(255 - thickness*20)
			drawLine(img, x1, y1, x2, y2, color.RGBA{0, 255, 100, alpha}, thickness+1)
		}
		for thickness := 4; thickness < 8; thickness++ {
			alpha := uint8(100 - thickness*10)
			drawLine(img, x1, y1, x2, y2, color.RGBA{0, 255, 100, alpha}, thickness+1)
		}
	}

	x1, y1 := toPixel(segment.StartLat, segment.StartLon)
	drawCircle(img, x1, y1, 10, color.RGBA{255, 50, 50, 255})
	drawCircle(img, x1, y1, 15, color.RGBA{255, 50, 50, 100})
	drawCircle(img, x1, y1, 20, color.RGBA{255, 50, 50, 50})

	x2, y2 := toPixel(segment.EndLat, segment.EndLon)
	drawCircle(img, x2, y2, 10, color.RGBA{50, 150, 255, 255})
	drawCircle(img, x2, y2, 15, color.RGBA{50, 150, 255, 100})
	drawCircle(img, x2, y2, 20, color.RGBA{50, 150, 255, 50})

	font, err := truetype.Parse(goregular.TTF)
	if err != nil {
		return nil, fmt.Errorf("ошибка загрузки шрифта: %w", err)
	}

	c := freetype.NewContext()
	c.SetDPI(72)
	c.SetFont(font)
	c.SetClip(img.Bounds())
	c.SetDst(img)

	c.SetFontSize(14)
	c.SetSrc(image.NewUniform(color.RGBA{255, 100, 100, 255}))
	pt := freetype.Pt(10, 30)
	c.DrawString("🚩 СТАРТ", pt)

	c.SetSrc(image.NewUniform(color.RGBA{100, 200, 255, 255}))
	pt = freetype.Pt(10, 50)
	c.DrawString("🏁 ФИНИШ", pt)

	c.SetSrc(image.NewUniform(color.RGBA{100, 255, 100, 255}))
	pt = freetype.Pt(10, 70)
	c.DrawString(fmt.Sprintf("📏 %.1f км", segment.ClearDistanceKm), pt)

	c.SetFontSize(10)
	c.SetSrc(image.NewUniform(color.RGBA{200, 200, 200, 200}))
	pt = freetype.Pt(10, 580)
	c.DrawString(fmt.Sprintf("📍 От: %.6f, %.6f", segment.StartLat, segment.StartLon), pt)

	pt = freetype.Pt(10, 595)
	c.DrawString(fmt.Sprintf("📍 До: %.6f, %.6f", segment.EndLat, segment.EndLon), pt)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (g *StaticMapGenerator) drawSimpleRoute(img *image.RGBA, segment service.Segment) ([]byte, error) {
	minLat := math.Min(segment.StartLat, segment.EndLat) - 0.01
	maxLat := math.Max(segment.StartLat, segment.EndLat) + 0.01
	minLon := math.Min(segment.StartLon, segment.EndLon) - 0.01
	maxLon := math.Max(segment.StartLon, segment.EndLon) + 0.01

	toPixel := func(lat, lon float64) (int, int) {
		x := int((lon-minLon)/(maxLon-minLon)*780) + 10
		y := int((1-(lat-minLat)/(maxLat-minLat))*580) + 10
		return x, y
	}

	x1, y1 := toPixel(segment.StartLat, segment.StartLon)
	x2, y2 := toPixel(segment.EndLat, segment.EndLon)

	drawLine(img, x1, y1, x2, y2, color.RGBA{0, 255, 100, 255}, 4)
	drawCircle(img, x1, y1, 10, color.RGBA{255, 50, 50, 255})
	drawCircle(img, x2, y2, 10, color.RGBA{50, 150, 255, 255})

	font, err := truetype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}

	c := freetype.NewContext()
	c.SetDPI(72)
	c.SetFont(font)
	c.SetClip(img.Bounds())
	c.SetDst(img)

	c.SetFontSize(14)
	c.SetSrc(image.NewUniform(color.RGBA{255, 100, 100, 255}))
	pt := freetype.Pt(10, 30)
	c.DrawString("🚩 СТАРТ", pt)

	c.SetSrc(image.NewUniform(color.RGBA{100, 200, 255, 255}))
	pt = freetype.Pt(10, 50)
	c.DrawString("🏁 ФИНИШ", pt)

	c.SetSrc(image.NewUniform(color.RGBA{100, 255, 100, 255}))
	pt = freetype.Pt(10, 70)
	c.DrawString(fmt.Sprintf("📏 %.1f км", segment.ClearDistanceKm), pt)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func drawLine(img *image.RGBA, x1, y1, x2, y2 int, col color.RGBA, thickness int) {
	dx := x2 - x1
	dy := y2 - y1
	steps := int(math.Sqrt(float64(dx*dx + dy*dy)))
	if steps == 0 {
		steps = 1
	}

	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := int(float64(x1) + t*float64(dx))
		y := int(float64(y1) + t*float64(dy))

		for dyOff := -thickness / 2; dyOff <= thickness/2; dyOff++ {
			for dxOff := -thickness / 2; dxOff <= thickness/2; dxOff++ {
				px := x + dxOff
				py := y + dyOff
				if px >= 0 && px < 800 && py >= 0 && py < 600 {
					img.Set(px, py, col)
				}
			}
		}
	}
}

func drawCircle(img *image.RGBA, cx, cy, radius int, col color.RGBA) {
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			if x*x+y*y <= radius*radius {
				px := cx + x
				py := cy + y
				if px >= 0 && px < 800 && py >= 0 && py < 600 {
					img.Set(px, py, col)
				}
			}
		}
	}
}
