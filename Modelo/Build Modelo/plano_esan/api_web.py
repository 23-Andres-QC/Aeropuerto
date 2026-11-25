"""Cliente de la web para los scripts del plano de ESAN, solo con la biblioteca estándar (corre también en el servidor, sin OpenCV)."""
import json
import os
import urllib.error
import urllib.request
from pathlib import Path

LLAVE_LOCAL = "lap-local-api-key"


def llave_api():
    """La llave con la que los scripts le hablan a la web, que pide acceso para todo (lee y escribe; no borra): LAP_API_KEY,
    o API_KEY del .env de la raíz del proyecto, o la de prueba de la laptop."""
    if os.environ.get("LAP_API_KEY"):
        return os.environ["LAP_API_KEY"]
    for carpeta in Path(__file__).resolve().parents:
        if (carpeta / "compose.yaml").exists():
            try:
                for linea in (carpeta / ".env").read_text(encoding="utf-8").splitlines():
                    if linea.startswith("API_KEY=") and linea[8:].strip():
                        return linea[8:].strip().strip("'\"")
            except OSError:
                pass
            break
    return LLAVE_LOCAL


def llamar(web, metodo, ruta, cuerpo=None):
    peticion = urllib.request.Request(f"{web.rstrip('/')}{ruta}", method=metodo,
                                      data=None if cuerpo is None else json.dumps(cuerpo).encode("utf-8"),
                                      headers={"Content-Type": "application/json", "Authorization": f"Bearer {llave_api()}"})
    try:
        with urllib.request.urlopen(peticion, timeout=30) as r:
            datos = r.read()
    except urllib.error.HTTPError as e:
        if e.code in (401, 403):
            raise SystemExit(f"{metodo} {ruta} → {e.code}. Falta la llave de la web: define LAP_API_KEY (la API_KEY del .env del servidor; en la laptop vale la de prueba).") from None
        raise
    return json.loads(datos) if datos else None
