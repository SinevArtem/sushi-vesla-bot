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

	"sushi-vesla-bot/internal/routing"
)

type StaticMapGenerator struct{}

func NewStaticMapGenerator() *StaticMapGenerator {
	return &StaticMapGenerator{}
}

func (g *StaticMapGenerator) GenerateRouteImage(startLat, startLon, endLat, endLon float64, geometry []routing.Coordinate) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{20, 20, 30, 255}}, image.Point{}, draw.Src)

	for x := 0; x < 800; x += 40 {
		for y := 0; y < 600; y += 40 {
			img.Set(x, y, color.RGBA{40, 40, 50, 255})
		}
	}

	if len(geometry) > 1 {
		return g.drawRouteWithGeometry(img, startLat, startLon, endLat, endLon, geometry)
	}

	return g.drawSimpleRoute(img, startLat, startLon, endLat, endLon)
}

func (g *StaticMapGenerator) drawRouteWithGeometry(
	img *image.RGBA,
	startLat, startLon, endLat, endLon float64,
	geometry []routing.Coordinate,
) ([]byte, error) {

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

	x1, y1 := toPixel(startLat, startLon)
	drawCircle(img, x1, y1, 10, color.RGBA{255, 50, 50, 255})
	drawCircle(img, x1, y1, 15, color.RGBA{255, 50, 50, 100})
	drawCircle(img, x1, y1, 20, color.RGBA{255, 50, 50, 50})

	x2, y2 := toPixel(endLat, endLon)
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

	dist := haversineDistance(startLat, startLon, endLat, endLon)

	c.SetFontSize(14)
	c.SetSrc(image.NewUniform(color.RGBA{255, 100, 100, 255}))
	pt := freetype.Pt(10, 30)
	c.DrawString("🚩 СТАРТ", pt)

	c.SetSrc(image.NewUniform(color.RGBA{100, 200, 255, 255}))
	pt = freetype.Pt(10, 50)
	c.DrawString("🏁 ФИНИШ", pt)

	c.SetSrc(image.NewUniform(color.RGBA{100, 255, 100, 255}))
	pt = freetype.Pt(10, 70)
	c.DrawString(fmt.Sprintf("📏 %.1f км", dist), pt)

	c.SetFontSize(10)
	c.SetSrc(image.NewUniform(color.RGBA{200, 200, 200, 200}))
	pt = freetype.Pt(10, 580)
	c.DrawString(fmt.Sprintf("📍 От: %.6f, %.6f", startLat, startLon), pt)

	pt = freetype.Pt(10, 595)
	c.DrawString(fmt.Sprintf("📍 До: %.6f, %.6f", endLat, endLon), pt)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (g *StaticMapGenerator) drawSimpleRoute(
	img *image.RGBA,
	startLat, startLon, endLat, endLon float64,
) ([]byte, error) {
	minLat := math.Min(startLat, endLat) - 0.01
	maxLat := math.Max(startLat, endLat) + 0.01
	minLon := math.Min(startLon, endLon) - 0.01
	maxLon := math.Max(startLon, endLon) + 0.01

	toPixel := func(lat, lon float64) (int, int) {
		x := int((lon-minLon)/(maxLon-minLon)*780) + 10
		y := int((1-(lat-minLat)/(maxLat-minLat))*580) + 10
		return x, y
	}

	x1, y1 := toPixel(startLat, startLon)
	x2, y2 := toPixel(endLat, endLon)

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

	dist := haversineDistance(startLat, startLon, endLat, endLon)
	c.SetSrc(image.NewUniform(color.RGBA{100, 255, 100, 255}))
	pt = freetype.Pt(10, 70)
	c.DrawString(fmt.Sprintf("📏 %.1f км", dist), pt)

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

func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}
