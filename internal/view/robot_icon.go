package view

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
)

// A small, self-contained robot favicon for the browser-hosted app title bar.
var robotIconPNG = makeRobotIcon()

func makeRobotIcon() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	ink := color.RGBA{24, 40, 48, 255}
	face := color.RGBA{99, 207, 191, 255}
	light := color.RGBA{237, 255, 250, 255}
	rect := func(x0, y0, x1, y1 int, c color.RGBA) {
		draw.Draw(img, image.Rect(x0, y0, x1, y1), &image.Uniform{C: c}, image.Point{}, draw.Src)
	}
	circle := func(cx, cy, r int, c color.RGBA) {
		for y := cy - r; y <= cy+r; y++ {
			for x := cx - r; x <= cx+r; x++ {
				if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r {
					img.SetRGBA(x, y, c)
				}
			}
		}
	}
	rect(29, 9, 35, 23, ink)
	circle(32, 8, 5, face)
	rect(3, 29, 11, 44, ink)
	rect(53, 29, 61, 44, ink)
	// Rounded head with a dark outline, readable even at 16 px.
	rect(16, 18, 48, 56, ink)
	rect(9, 25, 55, 49, ink)
	for _, p := range [][2]int{{16, 25}, {47, 25}, {16, 48}, {47, 48}} {
		circle(p[0], p[1], 7, ink)
	}
	rect(16, 22, 48, 52, face)
	rect(13, 26, 51, 48, face)
	for _, p := range [][2]int{{17, 26}, {46, 26}, {17, 47}, {46, 47}} {
		circle(p[0], p[1], 4, face)
	}
	circle(23, 34, 5, ink)
	circle(41, 34, 5, ink)
	circle(22, 33, 1, light)
	circle(40, 33, 1, light)
	rect(25, 44, 39, 47, ink)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func ServeRobotIcon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(robotIconPNG)
}
