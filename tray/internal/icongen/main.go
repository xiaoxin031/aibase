// Command icongen writes the original geometric aibase tray artwork.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
)

func main() {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	template := image.NewNRGBA(img.Bounds())
	for y := 2; y < 30; y++ {
		for x := 2; x < 30; x++ {
			if (x >= 5 && x < 27) || (y >= 5 && y < 27) {
				img.SetNRGBA(x, y, color.NRGBA{R: 35, G: 100, B: 220, A: 255})
			}
		}
	}
	for y := 4; y < 28; y++ {
		for x := 4; x < 28; x++ {
			if (y >= 22 && y < 27) || (x >= 6 && x < 11 && y >= 12) || (x >= 21 && x < 26 && y >= 12) || (y >= 5 && y < 10 && x >= 11 && x < 21) {
				img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
				template.SetNRGBA(x, y, color.NRGBA{A: 255})
			}
		}
	}
	var b bytes.Buffer
	must(png.Encode(&b, img))
	must(os.WriteFile("icons/tray.png", b.Bytes(), 0644))
	var ico bytes.Buffer
	for _, v := range []uint16{0, 1, 1} {
		must(binary.Write(&ico, binary.LittleEndian, v))
	}
	ico.Write([]byte{32, 32, 0, 0})
	for _, v := range []uint16{1, 32} {
		must(binary.Write(&ico, binary.LittleEndian, v))
	}
	for _, v := range []uint32{uint32(b.Len()), 22} {
		must(binary.Write(&ico, binary.LittleEndian, v))
	}
	ico.Write(b.Bytes())
	must(os.WriteFile("icons/tray.ico", ico.Bytes(), 0644))
	b.Reset()
	must(png.Encode(&b, template))
	must(os.WriteFile("icons/template.png", b.Bytes(), 0644))
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
