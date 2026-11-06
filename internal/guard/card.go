package guard

import (
	"bytes"
	"fmt"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"time"
)

var ink = color.RGBA{25, 42, 40, 255}
var muted = color.RGBA{77, 94, 90, 255}
var paper = color.RGBA{247, 249, 244, 255}
var teal = color.RGBA{13, 103, 88, 255}
var track = color.RGBA{221, 229, 219, 255}

type canvas struct {
	img          *image.RGBA
	normal, bold *opentype.Font
}

func newCanvas(h int) (*canvas, error) {
	n, e := opentype.Parse(goregular.TTF)
	if e != nil {
		return nil, e
	}
	b, e := opentype.Parse(gobold.TTF)
	if e != nil {
		return nil, e
	}
	c := &canvas{image.NewRGBA(image.Rect(0, 0, 1000, h)), n, b}
	draw.Draw(c.img, c.img.Bounds(), &image.Uniform{paper}, image.Point{}, draw.Src)
	return c, nil
}
func (c *canvas) text(x, y int, size float64, bold bool, s string, col color.Color) {
	f := c.normal
	if bold {
		f = c.bold
	}
	face, e := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if e != nil {
		return
	}
	defer face.Close()
	d := font.Drawer{Dst: c.img, Src: image.NewUniform(col), Face: face, Dot: fixed.P(x, y)}
	for d.MeasureString(s).Ceil() > 900-x && len([]rune(s)) > 1 {
		r := []rune(s)
		s = string(r[:len(r)-2]) + "…"
	}
	d.DrawString(s)
}
func (c *canvas) rect(x, y, w, h int, col color.Color) {
	draw.Draw(c.img, image.Rect(x, y, x+w, y+h), image.NewUniform(col), image.Point{}, draw.Src)
}
func (c *canvas) bar(y int, pct float64) {
	if math.IsNaN(pct) || math.IsInf(pct, 0) {
		pct = 0
	}
	pct = math.Max(0, math.Min(100, pct))
	col := teal
	if pct >= 90 {
		col = color.RGBA{162, 46, 34, 255}
	} else if pct >= 80 {
		col = color.RGBA{139, 82, 8, 255}
	}
	c.rect(56, y, 888, 14, track)
	c.rect(56, y, int(888*pct/100), 14, col)
}
func (c *canvas) encode() ([]byte, error) {
	var b bytes.Buffer
	e := png.Encode(&b, c.img)
	return b.Bytes(), e
}
func ResourceCard(s Snapshot) ([]byte, error) {
	h := 610 + len(s.GPUs)*155
	if len(s.GPUs) == 0 {
		h = 690
	}
	c, e := newCanvas(h)
	if e != nil {
		return nil, e
	}
	c.text(56, 60, 25, true, "Tavito Guard", teal)
	c.text(56, 115, 36, true, "Estado de recursos", ink)
	c.text(56, 156, 22, false, s.Host+"  ·  "+s.Time.UTC().Format("02 Jan 2006 15:04:05")+" UTC", muted)
	c.text(56, 220, 24, false, fmt.Sprintf("CPU · %d núcleos lógicos", s.Cores), muted)
	c.text(56, 285, 62, true, fmt.Sprintf("%.1f %%", s.CPU), ink)
	c.bar(310, s.CPU)
	c.text(56, 390, 24, false, "Memoria RAM", muted)
	c.text(56, 446, 48, true, Bytes(s.RAMUsed)+" / "+Bytes(s.RAMTotal), ink)
	c.bar(473, Percent(s.RAMUsed, s.RAMTotal))
	y := 540
	if len(s.GPUs) == 0 {
		c.text(56, y, 23, false, "GPU: no se detectó un dispositivo accesible.", muted)
	}
	for _, g := range s.GPUs {
		c.text(56, y, 24, true, g.Name, ink)
		busy := "Uso no disponible"
		if g.Busy != nil {
			busy = fmt.Sprintf("Uso %.1f %%", *g.Busy)
		}
		memory := "VRAM no disponible"
		if g.Total > 0 {
			memory = Bytes(g.Used) + " / " + Bytes(g.Total)
		}
		c.text(56, y+42, 23, false, busy+"  ·  "+memory, muted)
		if g.Temperature != nil {
			c.text(56, y+78, 22, false, fmt.Sprintf("Temperatura %.0f °C", *g.Temperature), muted)
		}
		y += 155
	}
	c.text(56, h-30, 20, false, fmt.Sprintf("Encendido %.1f h · CPU y RAM según /proc", s.Uptime/3600), muted)
	return c.encode()
}
func (c *canvas) line(x0, y0, x1, y1 int, col color.Color) {
	dx := abs(x1 - x0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	dy := -abs(y1 - y0)
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		c.rect(x0, y0, 3, 3, col)
		if x0 == x1 && y0 == y1 {
			break
		}
		e := 2 * err
		if e >= dy {
			err += dy
			x0 += sx
		}
		if e <= dx {
			err += dx
			y0 += sy
		}
	}
}
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
func HistoryCard(samples []Snapshot, now time.Time) ([]byte, error) {
	c, e := newCanvas(880)
	if e != nil {
		return nil, e
	}
	c.text(56, 60, 25, true, "Tavito Guard", teal)
	c.text(56, 115, 36, true, "Recursos · últimas 24 horas", ink)
	c.text(56, 155, 22, false, fmt.Sprintf("%d muestras reales · UTC", len(samples)), muted)
	if len(samples) < 2 {
		c.text(56, 260, 25, false, "El historial necesita al menos dos muestras.", ink)
		c.text(56, 305, 23, false, "Se guarda una muestra por minuto mientras el bot funciona.", muted)
		return c.encode()
	}
	start := now.Add(-24 * time.Hour)
	for panel := 0; panel < 2; panel++ {
		top := 235 + panel*300
		name := "CPU"
		if panel == 1 {
			name = "RAM"
		}
		c.text(56, top-25, 26, true, name+" (%)", ink)
		for _, v := range []int{0, 50, 100} {
			y := top + 200 - v*2
			c.text(56, y+7, 20, false, fmt.Sprint(v), muted)
			c.rect(115, y, 825, 1, track)
		}
		prevX, prevY := 0, 0
		var prevTime time.Time
		for _, s := range samples {
			if s.Time.Before(start) || s.Time.After(now) {
				continue
			}
			value := s.CPU
			if panel == 1 {
				value = Percent(s.RAMUsed, s.RAMTotal)
			}
			x := 115 + int(825*s.Time.Sub(start).Seconds()/86400)
			y := top + 200 - int(2*math.Max(0, math.Min(100, value)))
			if !prevTime.IsZero() && s.Time.Sub(prevTime) < 150*time.Second {
				c.line(prevX, prevY, x, y, teal)
			}
			c.rect(x, y, 4, 4, teal)
			prevX = x
			prevY = y
			prevTime = s.Time
		}
		c.text(115, top+235, 20, false, start.UTC().Format("15:04"), muted)
		c.text(820, top+235, 20, false, now.UTC().Format("15:04"), muted)
	}
	c.text(56, 850, 20, false, "Los espacios en las curvas indican períodos sin muestras.", muted)
	return c.encode()
}
func ResourceText(s Snapshot) string {
	out := fmt.Sprintf("CPU: %.1f %%\nRAM: %s / %s (%.1f %%)\n", s.CPU, Bytes(s.RAMUsed), Bytes(s.RAMTotal), Percent(s.RAMUsed, s.RAMTotal))
	for _, g := range s.GPUs {
		out += "GPU: " + g.Name
		if g.Busy != nil {
			out += fmt.Sprintf(" · %.1f %%", *g.Busy)
		}
		out += "\n"
	}
	return out
}
