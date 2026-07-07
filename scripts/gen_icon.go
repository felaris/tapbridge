//go:build ignore

// gen_icon.go generates a multi-size Windows .ico from a source PNG.
//
// Windows' system tray loads icons via LoadImageW, which requires a real .ico
// file — a PNG will not load. This tool downscales the source logo to the
// standard tray sizes and packs them as PNG-compressed frames (supported on
// Windows Vista and later; TapBridge targets Windows 10/11).
//
// Usage:
//
//	go run scripts/gen_icon.go assets/icon.png assets/icon.ico
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
)

var sizes = []int{16, 32, 48, 256}

func main() {
	if len(os.Args) != 3 {
		log.Fatalf("usage: go run scripts/gen_icon.go <src.png> <out.ico>")
	}
	src, out := os.Args[1], os.Args[2]

	f, err := os.Open(src)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		log.Fatal(err)
	}

	var frames [][]byte
	for _, s := range sizes {
		scaled := boxDownscale(img, s)
		var buf bytes.Buffer
		if err := png.Encode(&buf, scaled); err != nil {
			log.Fatal(err)
		}
		frames = append(frames, buf.Bytes())
	}

	ico := encodeICO(frames)
	if err := os.WriteFile(out, ico, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (%d sizes, %d bytes)", out, len(sizes), len(ico))
}

// boxDownscale resizes img to size×size by averaging the source pixels covered
// by each destination pixel (a simple area/box filter — good enough for a logo).
func boxDownscale(img image.Image, size int) *image.NRGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	for dy := 0; dy < size; dy++ {
		sy0 := b.Min.Y + dy*sh/size
		sy1 := b.Min.Y + (dy+1)*sh/size
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for dx := 0; dx < size; dx++ {
			sx0 := b.Min.X + dx*sw/size
			sx1 := b.Min.X + (dx+1)*sw/size
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var r, g, bl, a, n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					cr, cg, cb, ca := img.At(sx, sy).RGBA()
					r += uint64(cr)
					g += uint64(cg)
					bl += uint64(cb)
					a += uint64(ca)
					n++
				}
			}
			if n == 0 {
				n = 1
			}
			dst.SetNRGBA(dx, dy, color.NRGBA{
				R: uint8((r / n) >> 8),
				G: uint8((g / n) >> 8),
				B: uint8((bl / n) >> 8),
				A: uint8((a / n) >> 8),
			})
		}
	}
	return dst
}

// encodeICO packs PNG frames into an ICO container.
func encodeICO(frames [][]byte) []byte {
	var buf bytes.Buffer
	// ICONDIR
	binary.Write(&buf, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(&buf, binary.LittleEndian, uint16(len(frames)))

	offset := 6 + 16*len(frames)
	for i, data := range frames {
		s := sizes[i]
		dim := byte(s)
		if s >= 256 {
			dim = 0 // 0 means 256 in the ICO spec
		}
		buf.WriteByte(dim)                                            // width
		buf.WriteByte(dim)                                            // height
		buf.WriteByte(0)                                             // color count
		buf.WriteByte(0)                                             // reserved
		binary.Write(&buf, binary.LittleEndian, uint16(1))          // planes
		binary.Write(&buf, binary.LittleEndian, uint16(32))         // bit count
		binary.Write(&buf, binary.LittleEndian, uint32(len(data)))  // bytes in resource
		binary.Write(&buf, binary.LittleEndian, uint32(offset))     // image offset
		offset += len(data)
	}
	for _, data := range frames {
		buf.Write(data)
	}
	return buf.Bytes()
}
