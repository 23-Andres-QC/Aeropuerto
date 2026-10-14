'use strict';

// Comprime a JPEG los cuadros de la cámara fuera del hilo principal (ver comprimir en app.js): recibe el cuadro como
// ImageBitmap y devuelve el Blob, o el error para que la página vuelva a canvas.toBlob.
let lienzo = null;
let ctx = null;

onmessage = async (e) => {
  const { id, imagen, calidad } = e.data;
  try {
    if (!lienzo || lienzo.width !== imagen.width || lienzo.height !== imagen.height) {
      lienzo = new OffscreenCanvas(imagen.width, imagen.height);
      ctx = lienzo.getContext('2d');
    }
    ctx.drawImage(imagen, 0, 0);
    postMessage({ id, blob: await lienzo.convertToBlob({ type: 'image/jpeg', quality: calidad }) });
  } catch (error) {
    postMessage({ id, error: String(error) });
  } finally {
    imagen.close();
  }
};
