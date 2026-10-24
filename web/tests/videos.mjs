// Prueba de cómo se guardan en la base los videos que procesó el modelo (sesión VIDEO, como las capturas en vivo).
// Uso: node web/tests/videos.mjs  (Node 22.18 o más nuevo: lee TypeScript directamente; sale con error si algo falla)
import assert from "node:assert/strict";
import { gruposTerminados, idDeClave, sesionDeVideos } from "../src/features/videos/guardar.ts";

const resumen = (personas, extra = {}) => ({ id: "", nombre: "", modo: "tiempo_real", estado: "terminado", duracion_s: 60, personas, ...extra });
const video = (id, extra = {}) => ({ id, nombre: `${id}.mp4`, modo: "tiempo_real", bytes: 1, subido: "2026-10-20T15:00:00.000Z", resumen: null, ...extra });

// Un grupo se guarda cuando todos sus videos terminaron; la tanda va junta y el video suelto aparte.
const lista = [
  video("a", { tanda: "t1", en_tanda: 2, resumen: resumen([]) }),
  video("b", { tanda: "t1", en_tanda: 2 }),
  video("c", { resumen: resumen([]) }),
];
assert.deepEqual(gruposTerminados(lista).map((g) => g.clave), ["video:c"], "la tanda espera a que termine su segundo video");
lista[1].resumen = resumen([]);
assert.deepEqual(gruposTerminados(lista).map((g) => [g.clave, g.videos.length]), [["tanda:t1", 2], ["video:c", 1]]);

// El identificador es fijo por grupo (volver a guardar reemplaza la sesión) y tiene forma de UUID.
assert.equal(idDeClave("tanda:t1"), idDeClave("tanda:t1"));
assert.notEqual(idDeClave("tanda:t1"), idDeClave("tanda:t2"));
assert.match(idDeClave("video:vid-0123456789ab"), /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);

// Sin personas, o con todos los videos en error, no hay nada que guardar.
assert.equal(sesionDeVideos({ clave: "video:c", videos: [lista[2]] }), null);
assert.equal(sesionDeVideos({ clave: "video:x", videos: [video("x", { resumen: resumen([{ id: 1, genero: "Mujer", desde_s: 0, hasta_s: 5, visible_s: 5 }], { estado: "error" }) })] }), null);

// En una tanda, la misma ID en dos videos es una persona vista por dos cámaras; el género queda el de más certeza.
const tanda = {
  clave: "tanda:t9",
  videos: [
    video("p", { tanda: "t9", resumen: resumen([{ id: 7, genero: "Hombre", certeza: 0.6, desde_s: 10, hasta_s: 20, visible_s: 10 }, { id: 8, genero: "Sin determinar", desde_s: 1, hasta_s: 2, visible_s: 1 }]) }),
    video("q", { tanda: "t9", subido: "2026-10-20T15:00:30.000Z", resumen: resumen([{ id: 7, genero: "Mujer", certeza: 0.9, desde_s: 5, hasta_s: 40, visible_s: 30 }]) }),
    video("r", { tanda: "t9", resumen: resumen([], { estado: "error" }) }),
  ],
};
const carga = sesionDeVideos(tanda);
assert.equal(carga.session.kind, "VIDEO");
assert.equal(carga.session.session_id, idDeClave("tanda:t9"));
assert.equal(carga.session.recording_start, "2026-10-20T15:00:00.000Z");
assert.equal(carga.session.ended_at, "2026-10-20T15:01:30.000Z", "termina cuando acaba el último video");
assert.match(carga.session.name, /^video · .* · p\.mp4, q\.mp4$/, "el nombre lleva solo los videos que terminaron");
assert.equal(carga.identities.length, 2);
const siete = carga.identities.find((i) => i.public_number === 7);
assert.equal(siete.gender, "MUJER");
assert.equal(siete.gender_confidence, 0.9);
assert.equal(siete.n_cameras, 2);
assert.equal(siete.first_seen, "2026-10-20T15:00:10.000Z");
assert.equal(siete.last_seen, "2026-10-20T15:01:10.000Z");
assert.equal(carga.identities.find((i) => i.public_number === 8).gender, "SIN_DETERMINAR");
assert.equal(carga.points.t.length, 0, "los videos no dejan recorridos en el plano");

// El nombre cabe en los 120 bytes de la base aunque los archivos tengan nombres largos con tildes.
const largo = video("l", { nombre: `${"canción ñandú ".repeat(20)}.mp4`, resumen: resumen([{ id: 1, genero: "Hombre", certeza: 0.7, desde_s: 0, hasta_s: 1, visible_s: 1 }]) });
assert.ok(new TextEncoder().encode(sesionDeVideos({ clave: "video:l", videos: [largo] }).session.name).length <= 120);

console.log("videos: todo bien");
