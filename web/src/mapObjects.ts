import type { Feature, FeatureCollection, Geometry } from "geojson";
export type MapObject = {
  id: string;
  floor_id: number;
  kind: "zone" | "camera";
  name: string;
  category: string;
  color: string;
  source_ref: string;
  bearing: number;
  geometry: Geometry;
  coverage: GeoJSON.Polygon | null;
  revision: number;
};
export async function loadObjects(): Promise<MapObject[]> {
  const r = await fetch("/api/v1/map-objects?floor_id=3");
  if (!r.ok) throw Error("No se pudo cargar la configuración");
  return r.json();
}
export function features(objects: MapObject[]): FeatureCollection {
  const result: Feature[] = [];
  for (const o of objects) {
    const properties = { id: o.id, name: o.name, color: o.color, kind: o.kind };
    result.push({ type: "Feature", properties, geometry: o.geometry });
    if (o.coverage)
      result.push({
        type: "Feature",
        properties: { ...properties, kind: "coverage" },
        geometry: o.coverage,
      });
    if (o.kind === "camera" && o.geometry.type === "Point") {
      const [x, y] = o.geometry.coordinates;
      const a = (o.bearing * Math.PI) / 180;
      result.push({
        type: "Feature",
        properties: { ...properties, kind: "direction" },
        geometry: {
          type: "LineString",
          coordinates: [
            [x!, y!],
            [
              x! +
                (Math.sin(a) * 12) / (111320 * Math.cos((y! * Math.PI) / 180)),
              y! + (Math.cos(a) * 12) / 111320,
            ],
          ],
        },
      });
    }
  }
  return { type: "FeatureCollection", features: result };
}
