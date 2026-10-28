"""Paso 2 del ajuste del clasificador de género: calibra el sesgo y entrena una corrección lineal sobre los vectores de CLIP.

Lee ajuste_genero/{train,val,test}.npz (paso 1). Todo se decide con train/val; el test solo se usa para reportar.

Variantes (todas dan la probabilidad de «Mujer» como sigmoid(z), igual que el softmax de dos clases del pipeline):
  base       z = z0                      CLIP zero-shot, como está hoy en producción
  sesgo      z = z0 + b                  solo un desplazamiento, elegido en val para maximizar la balanced accuracy
  corregido  z = z0 + b + x·d            z0 + una capa lineal que parte de cero, con penalización lambda·|d|² (lambda elegido en val)
  libre      z = b + x·w                 regresión logística normal sobre los vectores (sin usar el texto), para comparar
donde z0 = escala·x·(t_mujer − t_hombre) es la diferencia de logits de CLIP con las frases del pipeline.

Uso (dentro del contenedor del modelo):
    python ajuste_genero_ajustar.py --carpeta /tm/ajuste_genero --pesos /modelo/clip_genero
"""
import argparse
import json
from pathlib import Path

import numpy as np
import torch

PROMPTS = (
    ("a photo of a man", "a photo of a male person", "a photo of a young man", "a man walking",
     "a full body photo of a man", "a cropped photo of a man", "a low resolution photo of a man"),
    ("a photo of a woman", "a photo of a female person", "a photo of a young woman", "a woman walking",
     "a full body photo of a woman", "a cropped photo of a woman", "a low resolution photo of a woman"),
)
UMBRAL = 0.55   # config_lap01.json -> gender.min_confidence con 2 clases


def cargar(carpeta, parte):
    d = np.load(carpeta / f"{parte}.npz", allow_pickle=False)
    atributos = [str(a) for a in d["atributos"]]
    return torch.from_numpy(d["x"]), d["y"], atributos


def textos(pesos):
    from transformers import CLIPModel, CLIPProcessor
    procesador = CLIPProcessor.from_pretrained(pesos, local_files_only=True)
    modelo = CLIPModel.from_pretrained(pesos, local_files_only=True).eval()
    clases = []
    for frases in PROMPTS:
        t = procesador(text=list(frases), return_tensors="pt", padding=True)
        with torch.inference_mode():
            f = modelo.get_text_features(**{k: v for k, v in t.items()}).pooler_output
        f = (f / f.norm(dim=-1, keepdim=True)).mean(dim=0, keepdim=True)
        clases.append(f / f.norm(dim=-1, keepdim=True))
    return torch.cat(clases), float(modelo.logit_scale.exp().detach())


def metricas(z, mujer):
    """z: logit de Mujer (tensor), mujer: 0/1 reales."""
    p = torch.sigmoid(z).numpy()
    pred = (p >= 0.5).astype(int)
    h, f = mujer == 0, mujer == 1
    rec_h, rec_f = float((pred[h] == 0).mean()), float((pred[f] == 1).mean())
    conf = np.maximum(p, 1 - p)
    pasa = conf >= UMBRAL
    return {"accuracy": float((pred == mujer).mean()), "balanced": (rec_h + rec_f) / 2, "recall_hombre": rec_h, "recall_mujer": rec_f,
            "cobertura_umbral": float(pasa.mean()), "accuracy_umbral": float((pred[pasa] == mujer[pasa]).mean())}


def entrenar(x, mujer, base, lam, balancear, libre=False, iteraciones=500):
    """LBFGS sobre BCE; devuelve (b, d). `base` es z0 (0 si es la variante libre)."""
    y = torch.from_numpy(mujer.astype(np.float32))
    peso = torch.ones_like(y)
    if balancear:
        peso = torch.where(y == 1, 0.5 / y.mean(), 0.5 / (1 - y.mean()))
    b = torch.zeros(1, requires_grad=True)
    d = torch.zeros(x.shape[1], requires_grad=True)
    opt = torch.optim.LBFGS([b, d], lr=1.0, max_iter=iteraciones, line_search_fn="strong_wolfe")

    def paso():
        opt.zero_grad()
        z = (0.0 if libre else base) + b + x @ d
        perdida = (torch.nn.functional.binary_cross_entropy_with_logits(z, y, reduction="none") * peso).mean() + lam * (d ** 2).sum()
        perdida.backward()
        return perdida

    opt.step(paso)
    return b.detach(), d.detach()


def mejor_sesgo(z0, mujer):
    candidatos = np.linspace(-3, 6, 181)
    pts = [(metricas(z0 + float(b), mujer)["balanced"], float(b)) for b in candidatos]
    return max(pts)[1]


def tabla(nombre, m):
    return (f"{nombre:<26} acc {m['accuracy'] * 100:5.2f}  balanced {m['balanced'] * 100:5.2f}  "
            f"recall H {m['recall_hombre'] * 100:5.2f}  M {m['recall_mujer'] * 100:5.2f}  "
            f"| umbral 0.55: cubre {m['cobertura_umbral'] * 100:5.1f} %, acc {m['accuracy_umbral'] * 100:5.2f}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--carpeta", default="/tm/ajuste_genero")
    ap.add_argument("--pesos", default="/modelo/clip_genero")
    args = ap.parse_args()
    carpeta = Path(args.carpeta)
    torch.manual_seed(0)
    xt, yt, at = cargar(carpeta, "train")
    xv, yv, _ = cargar(carpeta, "val")
    xs, ys, _ = cargar(carpeta, "test")
    fem = at.index("Female")
    mt, mv, ms = yt[:, fem], yv[:, fem], ys[:, fem]
    texto, escala = textos(args.pesos)
    dif = (texto[1] - texto[0])
    z0 = lambda x: escala * (x @ dif)
    zt, zv, zs = z0(xt), z0(xv), z0(xs)
    sal = [f"PA-100K · train {len(xt)} · val {len(xv)} · test {len(xs)} · escala CLIP {escala:.1f}",
           f"Mujeres: train {mt.mean() * 100:.1f} %  val {mv.mean() * 100:.1f} %  test {ms.mean() * 100:.1f} %", ""]

    resultados = {"base": metricas(zs, ms)}
    sal.append("== TEST ==")
    sal.append(tabla("base (producción hoy)", resultados["base"]))

    b = mejor_sesgo(zv, mv)
    resultados["sesgo"] = metricas(zs + b, ms)
    sal.append(tabla(f"sesgo b={b:+.2f}", resultados["sesgo"]))

    # corregido: lambda y balanceo se eligen por balanced accuracy en val
    mejor = None
    sal_val = ["", "== VAL (para elegir lambda) =="]
    for balancear in (False, True):
        for lam in (0.0, 1e-7, 1e-6, 3e-6, 1e-5, 1e-4, 1e-3):
            bb, d = entrenar(xt, mt, zt, lam, balancear)
            m = metricas(zv + bb + xv @ d, mv)
            sal_val.append(f"corregido lambda={lam:g} balanceado={balancear}:  acc {m['accuracy'] * 100:5.2f}  balanced {m['balanced'] * 100:5.2f}")
            if mejor is None or m["balanced"] > mejor[0]:
                mejor = (m["balanced"], lam, balancear, bb, d)
    _, lam, balancear, bb, d = mejor
    resultados["corregido"] = metricas(zs + bb + xs @ d, ms)
    sal.append(tabla(f"corregido l={lam:g} bal={balancear}", resultados["corregido"]))

    mejor_l = None
    for lam_l in (0.0, 1e-6, 1e-5, 1e-4):
        bl, wl = entrenar(xt, mt, 0.0, lam_l, True, libre=True)
        m = metricas(bl + xv @ wl, mv)
        sal_val.append(f"libre lambda={lam_l:g}:  acc {m['accuracy'] * 100:5.2f}  balanced {m['balanced'] * 100:5.2f}")
        if mejor_l is None or m["balanced"] > mejor_l[0]:
            mejor_l = (m["balanced"], lam_l, bl, wl)
    _, lam_l, bl, wl = mejor_l
    resultados["libre"] = metricas(bl + xs @ wl, ms)
    sal.append(tabla(f"libre l={lam_l:g}", resultados["libre"]))

    # Errores por vista y edad con la variante corregida frente a la base
    sal.append("")
    sal.append("== Error (%) del TEST por condición: base -> corregido ==")
    zc = zs + bb + xs @ d
    pb, pc = (torch.sigmoid(zs).numpy() >= 0.5).astype(int), (torch.sigmoid(zc).numpy() >= 0.5).astype(int)
    for nombre in ("Front", "Side", "Back", "AgeLess18", "Age18-60", "AgeOver60", "Glasses", "Hat", "BoldHead"):
        if nombre not in at:
            continue
        s = ys[:, at.index(nombre)] == 1
        if s.sum() < 30:
            continue
        sal.append(f"{nombre:<10} n={int(s.sum()):5d}   {100 * (pb[s] != ms[s]).mean():5.1f} -> {100 * (pc[s] != ms[s]).mean():5.1f}")

    salida = "\n".join(sal + sal_val)
    print(salida)
    (carpeta / "resultados.txt").write_text(salida, encoding="utf-8")
    (carpeta / "resultados.json").write_text(json.dumps(resultados, indent=2), encoding="utf-8")
    # Parámetros de la variante corregida: es la que entra al pipeline (ver ClasificadorGeneroCLIP.ajuste).
    (carpeta / "ajuste_lineal.json").write_text(json.dumps({
        "descripcion": "Corrección lineal del logit de Mujer sobre el vector de imagen de CLIP normalizado (PA-100K train).",
        "sesgo": float(bb), "pesos": [round(float(v), 6) for v in d], "lambda": lam, "balanceado": balancear,
        "sesgo_solo": float(b)}), encoding="utf-8")


if __name__ == "__main__":
    main()
