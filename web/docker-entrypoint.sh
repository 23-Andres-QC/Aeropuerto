#!/bin/sh
# Genera un certificado autofirmado en el primer arranque del contenedor
# (nunca se commitea una clave privada al repo). Vive en la capa escribible
# del contenedor: sobrevive a un restart, se regenera solo si se recrea.
set -e
CERT_DIR=/etc/nginx/certs
if [ ! -f "$CERT_DIR/selfsigned.crt" ]; then
  mkdir -p "$CERT_DIR"
  openssl req -x509 -nodes -newkey rsa:2048 -days 3650 \
    -keyout "$CERT_DIR/selfsigned.key" -out "$CERT_DIR/selfsigned.crt" \
    -subj "/CN=aeropuerto-demo" \
    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" 2>/dev/null
fi
exec nginx -g 'daemon off;'
