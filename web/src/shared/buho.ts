// El búho de TrackFlow, la mascota: una imagen por cara, con fondo transparente, en /buho/<cara>.png.
export type Pose = "neutral" | "guino" | "feliz" | "tableta" | "pensando" | "triste" | "enojado" | "sorpresa" | "idea" | "laptop" | "lupa" | "dormido";

/** Algo que dice el búho y la cara que pone mientras lo dice. */
export type Frase = { texto: string; pose?: Pose };

export const imagenBuho = (pose: Pose) => `/buho/${pose}.png`;
