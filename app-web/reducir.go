package main

import (
	"bytes"
	"image"
	"image/jpeg"
)

// Límites de ?ancho= en el MJPEG del modelo: menos no sirve para detectar y más es el tamaño de la página.
const (
	anchoMinimo  = 160
	anchoMaximo  = 4096
	calidadMJPEG = 70
)

// reducido devuelve el JPEG con a lo más `ancho` px de ancho (misma proporción), o el original si ya es angosto o no
// se puede decodificar. Lo pide el modelo cuando corre en otra máquina que la web (scripts/modelo_local.py): por
// internet, el cuadro entero de cada teléfono no alcanza a llegar a tiempo.
func reducido(frame []byte, ancho int) []byte {
	img, err := jpeg.Decode(bytes.NewReader(frame))
	if err != nil {
		return frame
	}
	b := img.Bounds()
	if b.Dx() <= ancho {
		return frame
	}
	alto := max(1, b.Dy()*ancho/b.Dx())
	var chico image.Image
	if ycc, ok := img.(*image.YCbCr); ok && b.Min == (image.Point{}) && ycc.SubsampleRatio == image.YCbCrSubsampleRatio420 {
		chico = reducirYCbCr(ycc, ancho, alto)
	} else {
		chico = reducirGeneral(img, ancho, alto)
	}
	var salida bytes.Buffer
	if jpeg.Encode(&salida, chico, &jpeg.Options{Quality: calidadMJPEG}) != nil {
		return frame
	}
	return salida.Bytes()
}

// reducirYCbCr promedia cada plano (luz y color) por área, sin pasar por RGB: es lo que mandan los navegadores.
func reducirYCbCr(src *image.YCbCr, ancho, alto int) *image.YCbCr {
	dst := image.NewYCbCr(image.Rect(0, 0, ancho, alto), image.YCbCrSubsampleRatio420)
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	plano(dst.Y, dst.YStride, ancho, alto, src.Y, src.YStride, sw, sh)
	cw, ch, scw, sch := (ancho+1)/2, (alto+1)/2, (sw+1)/2, (sh+1)/2
	plano(dst.Cb, dst.CStride, cw, ch, src.Cb, src.CStride, scw, sch)
	plano(dst.Cr, dst.CStride, cw, ch, src.Cr, src.CStride, scw, sch)
	return dst
}

// plano reduce un plano de sw×sh a dw×dh promediando el área de origen de cada píxel (dw ≤ sw, dh ≤ sh).
func plano(d []uint8, ds, dw, dh int, s []uint8, ss, sw, sh int) {
	for y := range dh {
		y0 := y * sh / dh
		y1 := max((y+1)*sh/dh, y0+1)
		for x := range dw {
			x0 := x * sw / dw
			x1 := max((x+1)*sw/dw, x0+1)
			suma := 0
			for yy := y0; yy < y1; yy++ {
				for _, v := range s[yy*ss+x0 : yy*ss+x1] {
					suma += int(v)
				}
			}
			d[y*ds+x] = uint8(suma / ((y1 - y0) * (x1 - x0)))
		}
	}
}

// reducirGeneral toma el píxel más cercano: para JPEG poco comunes (grises, otro submuestreo).
func reducirGeneral(src image.Image, ancho, alto int) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, ancho, alto))
	for y := range alto {
		for x := range ancho {
			dst.Set(x, y, src.At(b.Min.X+x*b.Dx()/ancho, b.Min.Y+y*b.Dy()/alto))
		}
	}
	return dst
}
