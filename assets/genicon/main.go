// genicon rasterizes the app mark into icon.ico and a 256px preview. The
// geometry is the SVG's (assets/icon.svg), drawn as signed distance fields so
// every size renders crisp from the same numbers instead of a downscaled
// bitmap; keep the two files in sync.
//
//	go run ./assets/genicon
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
)

// Geometry in 256-unit icon space: the accent plate, three ledger rows, and
// the strike that crosses them out.
const (
	plate        = 256.0
	cornerRadius = 58.0
)

var (
	accent = color.RGBA{0x35, 0x74, 0xF0, 0xFF}
	mark   = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}

	rows = []rect{
		{x: 60, y: 66, w: 136, h: 18, r: 9},
		{x: 60, y: 118, w: 104, h: 18, r: 9},
		{x: 60, y: 170, w: 80, h: 18, r: 9},
	}
	strike = seg{x1: 46, y1: 188, x2: 210, y2: 72, w: 20}
)

type rect struct{ x, y, w, h, r float64 }

type seg struct{ x1, y1, x2, y2, w float64 }

// sdRoundRect is the signed distance to a rounded rectangle (negative inside).
func sdRoundRect(px, py float64, r rect) float64 {
	cx, cy := r.x+r.w/2, r.y+r.h/2
	hw, hh := r.w/2-r.r, r.h/2-r.r
	dx, dy := math.Abs(px-cx)-hw, math.Abs(py-cy)-hh
	ox, oy := max(dx, 0), max(dy, 0)
	return math.Hypot(ox, oy) + min(max(dx, dy), 0) - r.r
}

// sdSeg is the signed distance to a capsule (a line with round caps).
func sdSeg(px, py float64, s seg) float64 {
	vx, vy := s.x2-s.x1, s.y2-s.y1
	wx, wy := px-s.x1, py-s.y1
	t := max(0, min(1, (wx*vx+wy*vy)/(vx*vx+vy*vy)))
	return math.Hypot(wx-t*vx, wy-t*vy) - s.w/2
}

// markDist is the signed distance to the whole white mark; the union of the
// rows and the strike.
func markDist(px, py float64) float64 {
	d := math.Inf(1)
	for _, r := range rows {
		d = min(d, sdRoundRect(px, py, r))
	}
	return min(d, sdSeg(px, py, strike))
}

// cover turns a signed distance into anti-aliased coverage over one pixel.
func cover(d float64) float64 {
	return max(0, min(1, 0.5-d))
}

func draw(size int) image.Image {
	scale := float64(size) / plate
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for py := range size {
		for px := range size {
			x, y := (float64(px)+0.5)/scale, (float64(py)+0.5)/scale
			plateD := sdRoundRect(x, y, rect{x: 0, y: 0, w: plate, h: plate, r: cornerRadius})
			c := color.RGBA{0, 0, 0, 0}
			if a := cover(plateD); a > 0 {
				c = blend(c, accent, a)
			}
			if a := cover(markDist(x, y)); a > 0 {
				c = blend(c, mark, a)
			}
			img.Set(px, py, c)
		}
	}
	return img
}

// blend is source-over with the source already straight-alpha.
func blend(dst color.RGBA, src color.RGBA, a float64) color.RGBA {
	da := float64(dst.A) / 255
	out := a + da*(1-a)
	r := (float64(src.R)*a + float64(dst.R)*da*(1-a)) / out
	g := (float64(src.G)*a + float64(dst.G)*da*(1-a)) / out
	b := (float64(src.B)*a + float64(dst.B)*da*(1-a)) / out
	return color.RGBA{uint8(r + 0.5), uint8(g + 0.5), uint8(b + 0.5), uint8(out*255 + 0.5)}
}

// sizes are the resolutions one ico carries; 256 is stored as 0 per the spec.
var sizes = []int{16, 24, 32, 48, 64, 128, 256}

func main() {
	// Write next to this source file: go run leaves the caller's working
	// directory alone, so a relative path would land wherever it was invoked.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot locate the generator source")
	}
	dir := filepath.Dir(filepath.Dir(file)) // assets/, beside the SVG
	var pngs [][]byte
	for _, s := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, draw(s)); err != nil {
			panic(err)
		}
		pngs = append(pngs, buf.Bytes())
		if s == 256 {
			if err := os.WriteFile(filepath.Join(dir, "icon-256.png"), pngs[len(pngs)-1], 0o666); err != nil {
				panic(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "icon.ico"), ico(pngs), 0o666); err != nil {
		panic(err)
	}
	// The object links into every Windows build; the version here is the
	// fallback for local builds, and a release regenerates it at its tag.
	strs := map[string]string{
		"CompanyName":      "xiyaowong",
		"FileDescription":  "清理可重建的缓存",
		"FileVersion":      "0.1.0",
		"LegalCopyright":   "xiyaowong · MIT",
		"ProductName":      "Outrash",
		"OriginalFilename": "outrash.exe",
	}
	root := filepath.Dir(dir)
	if err := os.WriteFile(filepath.Join(root, "rsrc_windows_amd64.syso"), buildSYSO(pngs, "0.1.0", strs), 0o666); err != nil {
		panic(err)
	}
}

// ico packs the PNGs into the container: one directory, one entry per size.
// PNG-compressed entries load on Windows Vista and later.
func ico(pngs [][]byte) []byte {
	var out bytes.Buffer
	u16 := func(v uint16) { binary.Write(&out, binary.LittleEndian, v) }
	u32 := func(v uint32) { binary.Write(&out, binary.LittleEndian, v) }
	u16(0) // reserved
	u16(1) // icon
	u16(uint16(len(pngs)))

	offset := uint32(6 + 16*len(pngs))
	for i, s := range sizes {
		b := byte(s)
		if s == 256 {
			b = 0
		}
		out.WriteByte(b)  // width
		out.WriteByte(b)  // height
		out.WriteByte(0)  // palette
		out.WriteByte(0)  // reserved
		u16(1)            // planes
		u16(32)           // bpp
		u32(uint32(len(pngs[i])))
		u32(offset)
		offset += uint32(len(pngs[i]))
	}
	for _, p := range pngs {
		out.Write(p)
	}
	return out.Bytes()
}
