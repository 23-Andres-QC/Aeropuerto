"""LAP01 · multitracking multicámara: un módulo por etapa del pipeline (fija YOLO_CONFIG_DIR antes de importar)."""
import os

os.environ.setdefault("YOLO_OFFLINE", "1")
os.environ.setdefault("YOLO_AUTOINSTALL", "0")

from .datos import *
from .deteccion_apariencia import *
from .tracking_local import *
from .camaras_reid import *
from .asociacion_multicamara import *
from .motor import *
from .mapa_2d import *
from .publicacion import *
