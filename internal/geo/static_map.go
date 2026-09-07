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
	"sort"
	"time"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font/gofont/goregular"

	"sushi-vesla-bot/internal/routing"
	"sushi-vesla-bot/internal/service"
)

type Tile struct {
	X, Y int
}

type StaticMapGenerator struct {
	httpClient *http.Client
}

func NewStaticMapGenerator() *StaticMapGenerator {
	return &StaticMapGenerator{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (g *StaticMapGenerator) GenerateRouteImage(segment service.Segment) ([]byte, error) {
	geometry := segment.Geometry
	if len(geometry) < 2 {
		return g.generateSchematicMap(segment)
	}

	imgBytes, err := g.generateOSMMap(segment)
	if err == nil {
		return imgBytes, nil
	}

	return g.generateSchematicMap(segment)
}

// generateOSMMap - получает реальную карту из OpenStreetMap
func (g *StaticMapGenerator) generateOSMMap(segment service.Segment) ([]byte, error) {
	geometry := segment.Geometry
	if len(geometry) < 2 {
		return nil, fmt.Errorf("геометрия маршрута слишком короткая")
	}

	// Находим границы маршрута
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

	// Уменьшаем отступы для большего приближения
	paddingLat := (maxLat - minLat) * 0.05
	paddingLon := (maxLon - minLon) * 0.05
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

	// Вычисляем zoom
	latDiff := maxLat - minLat
	lonDiff := maxLon - minLon
	zoom := 14
	if latDiff > 0.5 || lonDiff > 0.5 {
		zoom = 10
	} else if latDiff > 0.2 || lonDiff > 0.2 {
		zoom = 11
	} else if latDiff > 0.1 || lonDiff > 0.1 {
		zoom = 12
	} else if latDiff > 0.05 || lonDiff > 0.05 {
		zoom = 13
	} else if latDiff > 0.025 || lonDiff > 0.025 {
		zoom = 14
	} else if latDiff > 0.012 || lonDiff > 0.012 {
		zoom = 15
	} else {
		zoom = 16
	}

	// Получаем тайлы для маршрута
	tiles := g.getTilesForRoute(geometry, zoom)

	if len(tiles) == 0 {
		return nil, fmt.Errorf("не найдено тайлов для маршрута")
	}

	// Определяем границы тайлов
	minX, maxX := tiles[0].X, tiles[0].X
	minY, maxY := tiles[0].Y, tiles[0].Y
	for _, t := range tiles {
		if t.X < minX {
			minX = t.X
		}
		if t.X > maxX {
			maxX = t.X
		}
		if t.Y < minY {
			minY = t.Y
		}
		if t.Y > maxY {
			maxY = t.Y
		}
	}

	// Размеры сетки тайлов
	tileSize := 256
	gridWidth := maxX - minX + 1
	gridHeight := maxY - minY + 1
	imgWidth := gridWidth * tileSize
	imgHeight := gridHeight * tileSize

	// Создаём изображение
	fullImg := image.NewRGBA(image.Rect(0, 0, imgWidth, imgHeight))

	// Загружаем тайлы
	mirrors := []string{
		"https://tile.openstreetmap.org",
		"https://a.tile.openstreetmap.org",
		"https://b.tile.openstreetmap.org",
		"https://c.tile.openstreetmap.org",
	}

	for _, tile := range tiles {
		loaded := false
		for _, mirror := range mirrors {
			url := fmt.Sprintf("%s/%d/%d/%d.png", mirror, zoom, tile.X, tile.Y)

			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				continue
			}

			req.Header.Set("User-Agent", "SushiVeslaBot/1.0 (https://t.me/sushi_vesla_bot)")
			req.Header.Set("Accept", "image/png")

			resp, err := g.httpClient.Do(req)
			if err != nil {
				continue
			}

			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				continue
			}

			imgBytes, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				continue
			}

			tileImg, err := png.Decode(bytes.NewReader(imgBytes))
			if err != nil {
				continue
			}

			offsetX := (tile.X - minX) * tileSize
			offsetY := (tile.Y - minY) * tileSize

			for y := 0; y < tileSize; y++ {
				for x := 0; x < tileSize; x++ {
					fullImg.Set(offsetX+x, offsetY+y, tileImg.At(x, y))
				}
			}

			loaded = true
			break
		}

		if !loaded {
			offsetX := (tile.X - minX) * tileSize
			offsetY := (tile.Y - minY) * tileSize
			for y := 0; y < tileSize; y++ {
				for x := 0; x < tileSize; x++ {
					fullImg.Set(offsetX+x, offsetY+y, color.RGBA{220, 220, 220, 255})
				}
			}
		}
	}

	// Увеличенный размер картинки
	targetWidth := 1200
	targetHeight := 900

	scaleX := float64(targetWidth) / float64(imgWidth)
	scaleY := float64(targetHeight) / float64(imgHeight)
	scale := math.Min(scaleX, scaleY)

	newWidth := int(float64(imgWidth) * scale)
	newHeight := int(float64(imgHeight) * scale)

	offsetXImg := (targetWidth - newWidth) / 2
	offsetYImg := (targetHeight - newHeight) / 2

	scaledImg := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))

	// Фон
	for y := 0; y < targetHeight; y++ {
		for x := 0; x < targetWidth; x++ {
			scaledImg.Set(x, y, color.RGBA{240, 240, 240, 255})
		}
	}

	// Масштабируем карту
	for y := 0; y < newHeight; y++ {
		for x := 0; x < newWidth; x++ {
			srcX := int(float64(x) / scale)
			srcY := int(float64(y) / scale)
			if srcX < imgWidth && srcY < imgHeight {
				scaledImg.Set(offsetXImg+x, offsetYImg+y, fullImg.At(srcX, srcY))
			}
		}
	}

	// Вычисляем точные границы карты в координатах
	tileMinLon := g.tileToLon(minX, zoom)
	tileMaxLon := g.tileToLon(maxX+1, zoom)
	tileMinLat := g.tileToLat(maxY+1, zoom)
	tileMaxLat := g.tileToLat(minY, zoom)

	// Рисуем маршрут
	g.drawRouteOnMapCorrect(scaledImg, geometry, segment,
		tileMinLat, tileMaxLat, tileMinLon, tileMaxLon,
		offsetXImg, offsetYImg, newWidth, newHeight, scale)

	g.addTextOverlay(scaledImg, segment)

	var buf bytes.Buffer
	if err := png.Encode(&buf, scaledImg); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// tileToLat - преобразует Y тайла в широту
func (g *StaticMapGenerator) tileToLat(y, zoom int) float64 {
	n := math.Pi - 2.0*math.Pi*float64(y)/math.Pow(2.0, float64(zoom))
	return 180.0 / math.Pi * math.Atan(0.5*(math.Exp(n)-math.Exp(-n)))
}

// tileToLon - преобразует X тайла в долготу
func (g *StaticMapGenerator) tileToLon(x, zoom int) float64 {
	return float64(x)/math.Pow(2.0, float64(zoom))*360.0 - 180.0
}

// getTilesForRoute - возвращает тайлы для маршрута
func (g *StaticMapGenerator) getTilesForRoute(geometry []routing.Coordinate, zoom int) []Tile {
	tileMap := make(map[string]Tile)
	n := math.Pow(2, float64(zoom))

	for _, p := range geometry {
		xtile := int((p.Lon + 180) / 360 * n)
		ytile := int((1 - math.Log(math.Tan(p.Lat*math.Pi/180)+1/math.Cos(p.Lat*math.Pi/180))/math.Pi) / 2 * n)

		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				key := fmt.Sprintf("%d_%d", xtile+dx, ytile+dy)
				tileMap[key] = Tile{X: xtile + dx, Y: ytile + dy}
			}
		}
	}

	var tiles []Tile
	for _, t := range tileMap {
		tiles = append(tiles, t)
	}

	sort.Slice(tiles, func(i, j int) bool {
		if tiles[i].X == tiles[j].X {
			return tiles[i].Y < tiles[j].Y
		}
		return tiles[i].X < tiles[j].X
	})

	return tiles
}

// drawRouteOnMapCorrect - рисует маршрут с точным позиционированием
func (g *StaticMapGenerator) drawRouteOnMapCorrect(img *image.RGBA, geometry []routing.Coordinate, segment service.Segment, minLat, maxLat, minLon, maxLon float64, offsetX, offsetY, drawWidth, drawHeight int, scale float64) {
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()

	if maxLat <= minLat || maxLon <= minLon {
		return
	}

	if math.IsNaN(minLat) || math.IsNaN(maxLat) || math.IsNaN(minLon) || math.IsNaN(maxLon) {
		return
	}

	toPixel := func(lat, lon float64) (int, int) {
		normX := (lon - minLon) / (maxLon - minLon)
		normY := (lat - minLat) / (maxLat - minLat)
		normY = 1 - normY

		x := offsetX + int(normX*float64(drawWidth))
		y := offsetY + int(normY*float64(drawHeight))

		if x < 0 {
			x = 0
		}
		if x >= width {
			x = width - 1
		}
		if y < 0 {
			y = 0
		}
		if y >= height {
			y = height - 1
		}
		return x, y
	}

	// Рисуем маршрут с увеличенной толщиной
	for i := 0; i < len(geometry)-1; i++ {
		x1, y1 := toPixel(geometry[i].Lat, geometry[i].Lon)
		x2, y2 := toPixel(geometry[i+1].Lat, geometry[i+1].Lon)

		// Свечение - толще
		for thickness := 12; thickness >= 3; thickness-- {
			alpha := uint8(80 - thickness*3)
			if alpha < 10 {
				alpha = 10
			}
			drawLine(img, x1, y1, x2, y2, color.RGBA{255, 50, 50, alpha}, thickness)
		}

		// Основная линия - толще
		drawLine(img, x1, y1, x2, y2, color.RGBA{255, 0, 0, 255}, 6)
	}

	// Старт - больше
	startX, startY := toPixel(segment.StartLat, segment.StartLon)
	drawCircle(img, startX, startY, 12, color.RGBA{0, 255, 50, 255})
	drawCircle(img, startX, startY, 18, color.RGBA{0, 255, 50, 120})

	// Финиш - больше
	endX, endY := toPixel(segment.EndLat, segment.EndLon)
	drawCircle(img, endX, endY, 12, color.RGBA{50, 150, 255, 255})
	drawCircle(img, endX, endY, 18, color.RGBA{50, 150, 255, 120})
}

// addTextOverlay - текст на карте
func (g *StaticMapGenerator) addTextOverlay(img *image.RGBA, segment service.Segment) {
	font, err := truetype.Parse(goregular.TTF)
	if err != nil {
		return
	}

	c := freetype.NewContext()
	c.SetDPI(72)
	c.SetFont(font)
	c.SetClip(img.Bounds())
	c.SetDst(img)

	// Фон для текста
	textBg := image.NewRGBA(image.Rect(0, 0, 320, 90))
	draw.Draw(textBg, textBg.Bounds(), &image.Uniform{color.RGBA{0, 0, 0, 200}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(10, 10, 330, 100), textBg, image.Point{}, draw.Over)

	// Увеличиваем шрифт для лучшей видимости
	c.SetFontSize(16)
	c.SetSrc(image.NewUniform(color.RGBA{255, 255, 255, 255}))
	pt := freetype.Pt(15, 30)
	c.DrawString(fmt.Sprintf("📏 %.1f км", segment.DistanceKm), pt)

	c.SetFontSize(12)
	c.SetSrc(image.NewUniform(color.RGBA{0, 255, 50, 255}))
	pt = freetype.Pt(15, 52)
	c.DrawString(fmt.Sprintf("🚩 %.6f", segment.StartLat), pt)
	pt = freetype.Pt(15, 68)
	c.DrawString(fmt.Sprintf("   %.6f", segment.StartLon), pt)

	c.SetSrc(image.NewUniform(color.RGBA{50, 150, 255, 255}))
	pt = freetype.Pt(170, 52)
	c.DrawString(fmt.Sprintf("🏁 %.6f", segment.EndLat), pt)
	pt = freetype.Pt(170, 68)
	c.DrawString(fmt.Sprintf("   %.6f", segment.EndLon), pt)

	c.SetFontSize(9)
	c.SetSrc(image.NewUniform(color.RGBA{150, 150, 150, 150}))
	pt = freetype.Pt(10, img.Bounds().Dy()-10)
	c.DrawString("© OpenStreetMap contributors", pt)
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
			drawLine(img, x1, y1, x2, y2, color.RGBA{255, 0, 0, alpha}, thickness+1)
		}
		for thickness := 4; thickness < 8; thickness++ {
			alpha := uint8(100 - thickness*10)
			drawLine(img, x1, y1, x2, y2, color.RGBA{255, 0, 0, alpha}, thickness+1)
		}
	}

	x1, y1 := toPixel(segment.StartLat, segment.StartLon)
	drawCircle(img, x1, y1, 8, color.RGBA{0, 255, 50, 255})
	drawCircle(img, x1, y1, 12, color.RGBA{0, 255, 50, 100})

	x2, y2 := toPixel(segment.EndLat, segment.EndLon)
	drawCircle(img, x2, y2, 8, color.RGBA{50, 150, 255, 255})
	drawCircle(img, x2, y2, 12, color.RGBA{50, 150, 255, 100})

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

	c.SetSrc(image.NewUniform(color.RGBA{255, 50, 50, 255}))
	pt = freetype.Pt(10, 70)
	c.DrawString(fmt.Sprintf("📏 %.1f км", segment.DistanceKm), pt)

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

	drawLine(img, x1, y1, x2, y2, color.RGBA{255, 0, 0, 255}, 4)
	drawCircle(img, x1, y1, 8, color.RGBA{0, 255, 50, 255})
	drawCircle(img, x2, y2, 8, color.RGBA{50, 150, 255, 255})

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

	c.SetSrc(image.NewUniform(color.RGBA{255, 50, 50, 255}))
	pt = freetype.Pt(10, 70)
	c.DrawString(fmt.Sprintf("📏 %.1f км", segment.DistanceKm), pt)

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
				if px >= 0 && px < 1200 && py >= 0 && py < 900 {
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
				if px >= 0 && px < 1200 && py >= 0 && py < 900 {
					img.Set(px, py, col)
				}
			}
		}
	}
}
