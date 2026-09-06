<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from "vue";
import maplibregl, {
  type StyleSpecification,
  type GeoJSONSource,
  type FilterSpecification,
} from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
type Point = { x: number; y: number };
const props = defineProps<{
  people: (Point & { id: string; trail: Point[] })[];
  zones: {
    id: string;
    name: string;
    kind: string;
    geometry: { coordinates: number[][][] };
  }[];
  trails: boolean;
  selected: string;
}>();
const element = ref<HTMLDivElement>(),
  error = ref(""),
  ready = ref(false);
let map: maplibregl.Map | undefined,
  disposed = false;
const controller = new AbortController();
const mapURL =
  "https://map.lima-airport.com/?floor=3&lang=es-PE#15.98/-12.028493/-77.116766/-25.8";
// Illustrative local placement, NOT camera calibration. X/Y are demo units.
// The geographic anchor and rotation only place the synthetic scenario over the terminal.
function geo(p: Point): [number, number] {
  const angle = (-25.8 * Math.PI) / 180;
  const x = (p.x - 50) * 1.4,
    y = (38 - p.y) * 1.4;
  const east = x * Math.cos(angle) + y * Math.sin(angle),
    north = -x * Math.sin(angle) + y * Math.cos(angle);
  return [
    -77.116766 + east / (111320 * Math.cos((-12.028493 * Math.PI) / 180)),
    -12.028493 + north / 111320,
  ];
}
function update() {
  if (!map || !ready.value) return;
  const features: GeoJSON.Feature[] = [];
  for (const z of props.zones)
    features.push({
      type: "Feature",
      properties: { kind: z.kind, name: z.name },
      geometry: {
        type: "Polygon",
        coordinates: z.geometry.coordinates.map((r) =>
          r.map((p) => geo({ x: p[0]!, y: p[1]! })),
        ),
      },
    });
  for (const p of props.people) {
    features.push({
      type: "Feature",
      properties: {
        kind: "person",
        id: p.id,
        selected: p.id === props.selected,
      },
      geometry: { type: "Point", coordinates: geo(p) },
    });
    if (props.trails && p.trail.length > 1)
      features.push({
        type: "Feature",
        properties: { kind: "trail" },
        geometry: { type: "LineString", coordinates: p.trail.map(geo) },
      });
  }
  (map.getSource("simulation") as GeoJSONSource).setData({
    type: "FeatureCollection",
    features,
  });
}
function overview() {
  map?.flyTo({ center: [-77.116766, -12.028493], zoom: 15.98, bearing: -25.8 });
}
function focus() {
  map?.flyTo({ center: geo({ x: 50, y: 32 }), zoom: 18.6, bearing: -25.8 });
}
onMounted(async () => {
  try {
    const response = await fetch(
      "https://map-api.prod.livingmap.com/v1/maps/lima_airport_two/styles/styles.json",
      { signal: controller.signal },
    );
    if (!response.ok) throw Error("map unavailable");
    const style = (await response.json()) as StyleSpecification;
    // Match the public viewer's floor filter: selected floor + outdoor ground features.
    const floor: FilterSpecification = [
      "any",
      ["==", ["get", "floor_id"], 3],
      ["==", ["get", "floor_id"], ""],
      ["!", ["has", "floor_id"]],
      [
        "all",
        ["==", ["get", "floor_id"], 1],
        ["==", ["get", "indoor"], false],
        ["!", ["has", "pin_id"]],
      ],
    ];
    style.layers = style.layers.filter(
      (l) => !("source" in l) || ["lm", "lm_osm"].includes(l.source as string),
    );
    for (const layer of style.layers) {
      if ("source" in layer && layer.source === "lm" && "filter" in layer) {
        layer.filter = (
          layer.filter ? ["all", layer.filter, floor] : floor
        ) as FilterSpecification;
      } else if ("source" in layer && layer.source === "lm") {
        (layer as unknown as { filter: FilterSpecification }).filter = floor;
      }
    }
    for (const source of Object.values(style.sources)) {
      if ("tiles" in source && source.tiles)
        source.tiles = source.tiles.map((u) =>
          u.replace("lang=en-GB", "lang=es-PE"),
        );
    }
    if (disposed) return;
    map = new maplibregl.Map({
      container: element.value!,
      style,
      center: [-77.116766, -12.028493],
      zoom: 15.98,
      bearing: -25.8,
      minZoom: 14,
      maxZoom: 21,
      attributionControl: false,
    });
    map.addControl(new maplibregl.NavigationControl(), "top-right");
    map.addControl(
      new maplibregl.AttributionControl({
        compact: false,
        customAttribution:
          'Mapa: <a href="https://map.lima-airport.com/" target="_blank" rel="noopener">Lima Airport</a> · <a href="https://www.livingmap.com/" target="_blank" rel="noopener">Living Map</a>',
      }),
    );
    map.on("error", () => {
      error.value =
        "No se pudieron cargar algunas capas del mapa. Comprueba tu conexión o abre el visor original.";
    });
    map.on("load", () => {
      if (!map) return;
      map.addSource("simulation", {
        type: "geojson",
        data: { type: "FeatureCollection", features: [] },
      });
      map.addLayer({
        id: "demo-zones",
        type: "fill",
        source: "simulation",
        filter: ["in", ["get", "kind"], ["literal", ["shop", "front"]]],
        paint: {
          "fill-color": [
            "match",
            ["get", "kind"],
            "shop",
            "#17a777",
            "#7773db",
          ],
          "fill-opacity": 0.35,
        },
      });
      map.addLayer({
        id: "demo-zones-outline",
        type: "line",
        source: "simulation",
        filter: ["in", ["get", "kind"], ["literal", ["shop", "front"]]],
        paint: {
          "line-color": "#406c9e",
          "line-width": 1.5,
          "line-dasharray": [3, 2],
        },
      });
      map.addLayer({
        id: "demo-trails",
        type: "line",
        source: "simulation",
        filter: ["==", ["get", "kind"], "trail"],
        paint: {
          "line-color": "#1468d4",
          "line-width": 2,
          "line-opacity": 0.5,
        },
      });
      map.addLayer({
        id: "demo-people",
        type: "circle",
        source: "simulation",
        filter: ["==", ["get", "kind"], "person"],
        paint: {
          "circle-color": "#1468d4",
          "circle-radius": ["case", ["get", "selected"], 8, 5],
          "circle-stroke-color": "white",
          "circle-stroke-width": 2,
        },
      });
      ready.value = true;
      update();
    });
  } catch (e) {
    if (!disposed)
      error.value =
        "El mapa real no está disponible. Puedes abrir el visor original o recargar la página.";
  }
});
watch(() => [props.people, props.zones, props.trails, props.selected], update, {
  deep: true,
});
onUnmounted(() => {
  disposed = true;
  controller.abort();
  map?.remove();
});
</script>
<template>
  <div class="real-map-wrap">
    <div class="map-tools">
      <button @click="overview" :disabled="!ready">Ver terminal</button
      ><button @click="focus" :disabled="!ready">Acercar simulación</button
      ><a :href="mapURL" target="_blank" rel="noopener">Visor original ↗</a>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-else-if="!ready" class="map-loading" role="status">
      Cargando mapa real del nivel 3…
    </p>
    <div
      ref="element"
      class="real-map"
      aria-label="Mapa real del aeropuerto Jorge Chávez, nivel 3"
    ></div>
    <p class="map-disclaimer">
      Plano real · Zonas y recorridos simulados, con ubicación ilustrativa sin
      calibración.
    </p>
  </div>
</template>
