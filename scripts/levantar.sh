#!/usr/bin/env bash
# Levanta el proyecto con la configuración del lugar donde corre:
#   laptop   -> los valores por defecto de compose.yaml: web en 127.0.0.1:8080
#               (HTTPS 8443), cámara web en :8444 y el modelo en vivo fuera de
#               Docker, con la GPU.
#   servidor -> servidor.env (web en 80/443, modelo en vivo en CPU) más la IP
#               pública de la VM, que se lee de Google Cloud en cada arranque:
#               si la VM cambia de IP al encenderse, basta volver a correrlo.
# Reconoce solo la VM de Google Cloud; `scripts/levantar.sh local` o
# `scripts/levantar.sh servidor` lo fuerza.
set -euo pipefail
cd "$(dirname "$0")/.."

entorno=${1:-}
if [ -z "$entorno" ]; then
  if grep -qs 'Google Compute Engine' /sys/class/dmi/id/product_name; then
    entorno=servidor
  else
    entorno=local
  fi
fi

case "$entorno" in
local)
  docker compose up -d --build
  echo "Listo (local): http://localhost:${WEB_PORT:-8080}"
  ;;
servidor)
  if [ ! -f .env ]; then
    # Primera vez en esta VM: contraseñas aleatorias para las dos bases. La
    # contraseña se fija al crear la base, así que con bases ya creadas no se
    # puede inventar otra.
    if docker volume ls -q | grep -qE '^aeropuerto-demo_(vivo-)?pgdata$'; then
      echo "Falta .env con DB_PASSWORD y VIVO_DB_PASSWORD de las bases que ya existen." >&2
      exit 1
    fi
    aleatoria() { od -An -N16 -tx1 /dev/urandom | tr -d ' \n'; }
    (umask 077 && printf '# Contraseñas de las bases de este servidor (no versionado; ver servidor.env).\nDB_PASSWORD=%s\nVIVO_DB_PASSWORD=%s\n' \
      "$(aleatoria)" "$(aleatoria)" > .env)
    echo "Creado .env con contraseñas nuevas para las bases."
  fi
  # La IP detectada le gana a la de .env (una IP efímera cambia al apagar la
  # VM); para fijar otra dirección, pasar PUBLIC_HOST al llamar al script.
  if ip=$(curl -fsS -m 3 -H 'Metadata-Flavor: Google' \
      http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip); then
    export PUBLIC_HOST=${PUBLIC_HOST:-$ip}
    export ENLACE_CAMARA=${ENLACE_CAMARA:-https://$PUBLIC_HOST/camara/}
  else
    echo "Aviso: no se pudo leer la IP pública de la VM; se usa la de .env." >&2
  fi
  docker compose --env-file servidor.env --env-file .env up -d --build
  echo "Listo (servidor): http://${PUBLIC_HOST:-<IP pública>}/"
  ;;
*)
  echo "Uso: scripts/levantar.sh [local|servidor]" >&2
  exit 2
  ;;
esac
