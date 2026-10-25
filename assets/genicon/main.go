// genicon rasterizes the app mark into icon.ico and a 256px preview. The
// geometry is the SVG's (assets/icon.svg), drawn as signed distance fields so
// every size renders crisp from the same numbers instead of a downscaled
// bitmap; keep the two files in sync. The 256px PNG also lands in
// resources/, which `mygo build` reads for the packaged app's icon — without
// this write it would keep whatever the last manual copy left there.
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

// Geometry in 256-unit icon space: the accent plate, the checked box, and
// the check knocked out of it — its tail runs past the box and vanishes
// into the plate.
const (
	plate        = 256.0
	cornerRadius = 58.0
)

var (
	accent = color.RGBA{0x09, 0x69, 0xDA, 0xFF} // Primer accent.emphasis
	mark   = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}

	checkPlate = rect{x: 56, y: 56, w: 144, h: 144, r: 36}
	check      = []seg{
		{x1: 86, y1: 132, x2: 120, y2: 168, w: 26},
		{x1: 120, y1: 168, x2: 196, y2: 84, w: 26},
	}
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

// checkDist is the signed distance to the check knocked out of the box.
func checkDist(px, py float64) float64 {
	d := math.Inf(1)
	for _, s := range check {
		d = min(d, sdSeg(px, py, s))
	}
	return d
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
			if a := cover(sdRoundRect(x, y, checkPlate)); a > 0 {
				c = blend(c, mark, a)
			}
			if a := cover(checkDist(x, y)); a > 0 {
				c = blend(c, accent, a)
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
	// The object links into every Windows build. The version comes from the
	// environment so a release stamps its tag; a local build falls back to
	// this placeholder, which the properties sheet reads either way.
	version := os.Getenv("OUTRASH_VERSION")
	if version == "" {
		version = "0.1.0"
	}
	strs := map[string]string{
		"CompanyName":      "xiyaowong",
		"FileDescription":  "清理可重建的缓存",
		"FileVersion":      version,
		"LegalCopyright":   "xiyaowong · MIT",
		"ProductName":      "Outrash",
		"OriginalFilename": "outrash.exe",
	}
	root := filepath.Dir(dir)
	// The packaged app's icon: mygo reads resources/icon.png for the .app,
	// the Linux bundle and the Windows installer's shortcuts.
	if err := os.WriteFile(filepath.Join(root, "resources", "icon.png"), pngs[len(pngs)-1], 0o666); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rsrc_windows_amd64.syso"), buildSYSO(pngs, version, strs), 0o666); err != nil {
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
