"""Hotspots mediante Kernel Density Estimation sobre el plano."""
import numpy as np


def kde_grilla(x, y, pesos, origen, columnas, filas, celda_m, h_m, radio_h=4.0):
    """KDE gaussiano ponderado evaluado en el centro de cada celda de la grilla.

        f(x, y) = Σ w_i · K((x − X_i)/h, (y − Y_i)/h) / h²,   K(u) = exp(−|u|²/2) / 2π

    Con pesos que suman el total que se quiere repartir (segundos-persona, o 1 por persona),
    f queda en esas unidades por m². `origen` es la esquina inferior izquierda; el resultado
    se aplana por filas (de abajo hacia arriba). Solo se suman los puntos a menos de
    `radio_h` anchos de banda por eje de cada celda: con 4 se pierde ~0.01 % del peso.
    """
    x, y, w = (np.asarray(v, dtype=float) for v in (x, y, pesos))
    cx = origen[0] + (np.arange(columnas) + 0.5) * celda_m
    cy = origen[1] + (np.arange(filas) + 0.5) * celda_m
    densidad = np.zeros((filas, columnas))
    norma = 1.0 / (2 * np.pi * h_m * h_m)
    for i in range(0, len(x), 4096):
        xs, ys, ws = x[i:i + 4096], y[i:i + 4096], w[i:i + 4096]
        # Separable: exp(−(dx² + dy²)/2h²) = exp(−dx²/2h²)·exp(−dy²/2h²).
        dx = (cx[None, :] - xs[:, None]) / h_m
        dy = (cy[None, :] - ys[:, None]) / h_m
        kx = np.where(np.abs(dx) <= radio_h, np.exp(-0.5 * dx * dx), 0.0)
        ky = np.where(np.abs(dy) <= radio_h, np.exp(-0.5 * dy * dy), 0.0)
        densidad += np.einsum("n,nf,nc->fc", ws, ky, kx)
    return (densidad * norma).ravel()
