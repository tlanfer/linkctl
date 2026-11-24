// Package icon renders the linkctl app icon at runtime, so no binary assets
// are needed in the repository.
package icon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

var (
	accent  = color.NRGBA{0xFF, 0x4D, 0x2E, 0xFF}
	surface = color.NRGBA{0x1C, 0x1C, 0x1E, 0xFF}
	ring    = color.NRGBA{0xE8, 0xE8, 0xE8, 0xFF}
)

// PNG renders the colour icon: a dark rounded square with a camera lens.
func PNG(size int) []byte { return encode(render(size, false)) }

// TemplatePNG renders a monochrome (black + alpha) icon for the macOS menu bar.
func TemplatePNG(size int) []byte { return encode(render(size, true)) }

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func render(size int, template bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	c := s / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			d := math.Hypot(px-c, py-c)
			var col color.NRGBA
			var a float64
			if template {
				a = math.Max(band(d, s*0.30, s*0.42), disc(d, s*0.13))
			} else {
				a = roundedRect(px, py, s, s*0.22)
				col = surface
				if r := band(d, s*0.24, s*0.33); r > 0 {
					col = blend(col, ring, r)
				}
				if dot := disc(d, s*0.11); dot > 0 {
					col = blend(col, accent, dot)
				}
			}
			col.A = uint8(math.Round(a * 255))
			img.SetNRGBA(x, y, col)
		}
	}
	return img
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// disc and band return anti-aliased coverage for a filled circle / ring.
func disc(d, r float64) float64 { return clamp01(r - d + 0.5) }

func band(d, r0, r1 float64) float64 { return math.Min(clamp01(d-r0+0.5), clamp01(r1-d+0.5)) }

func roundedRect(x, y, s, r float64) float64 {
	dx := math.Max(math.Max(r-x, x-(s-r)), 0)
	dy := math.Max(math.Max(r-y, y-(s-r)), 0)
	return clamp01(r - math.Hypot(dx, dy) + 0.5)
}

func blend(a, b color.NRGBA, t float64) color.NRGBA {
	mix := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*(1-t) + float64(y)*t)) }
	return color.NRGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 0xFF}
}

// ICO wraps PNG images in a Windows .ico container (PNG-compressed entries
// are supported since Vista). With no usable input, 16/32/48/256 px icons
// are rendered.
func ICO(pngs ...[]byte) []byte {
	var imgs [][]byte
	for _, p := range pngs {
		if len(p) > 0 {
			imgs = append(imgs, p)
		}
	}
	if len(imgs) == 0 {
		imgs = [][]byte{PNG(16), PNG(32), PNG(48), PNG(256)}
	}
	var buf bytes.Buffer
	le := binary.LittleEndian
	_ = binary.Write(&buf, le, [3]uint16{0, 1, uint16(len(imgs))})
	offset := 6 + 16*len(imgs)
	dim := func(v int) uint8 {
		if v >= 256 {
			return 0 // 0 means 256 in ICO headers
		}
		return uint8(v)
	}
	for _, p := range imgs {
		cfg, _ := png.DecodeConfig(bytes.NewReader(p))
		buf.Write([]byte{dim(cfg.Width), dim(cfg.Height), 0, 0})
		_ = binary.Write(&buf, le, [2]uint16{1, 32})
		_ = binary.Write(&buf, le, [2]uint32{uint32(len(p)), uint32(offset)})
		offset += len(p)
	}
	for _, p := range imgs {
		buf.Write(p)
	}
	return buf.Bytes()
}
