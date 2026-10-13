package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func jpegDe(t *testing.T, ancho, alto int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, ancho, alto))
	for y := range alto {
		for x := range ancho {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestReducidoBajaElAnchoYConservaLaProporcion(t *testing.T) {
	original := jpegDe(t, 1280, 960)
	chico := reducido(original, 640)
	img, err := jpeg.Decode(bytes.NewReader(chico))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 640 || b.Dy() != 480 {
		t.Fatalf("tamaño = %v, se esperaba 640x480", b.Size())
	}
	if len(chico) >= len(original) {
		t.Fatalf("no se achicó: %d -> %d bytes", len(original), len(chico))
	}
	// El color se conserva: el promedio por área no corre los valores.
	r, g, _, _ := img.At(64, 64).RGBA() // viene de (128, 128) del original
	if d := int(r>>8) - 128; d < -12 || d > 12 {
		t.Fatalf("rojo en (64, 64) = %d, se esperaba ~128", r>>8)
	}
	if d := int(g>>8) - 128; d < -12 || d > 12 {
		t.Fatalf("verde en (64, 64) = %d, se esperaba ~128", g>>8)
	}
}

func TestReducidoDejaIgualLoAngostoYLoQueNoEsJPEG(t *testing.T) {
	angosto := jpegDe(t, 320, 240)
	if !bytes.Equal(reducido(angosto, 640), angosto) {
		t.Fatal("un cuadro más angosto que el pedido no se toca")
	}
	basura := []byte("no es un jpeg")
	if !bytes.Equal(reducido(basura, 640), basura) {
		t.Fatal("lo que no se puede decodificar pasa tal cual")
	}
}
