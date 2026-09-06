<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import type { MapObject } from "./mapObjects";
const props = defineProps<{
  camera: MapObject;
  taken?: string[];
  // Only the editor may change which physical device a camera uses.
  configurable?: boolean;
  expandable?: boolean;
  expanded?: boolean;
}>();
const emit = defineEmits<{ assign: [deviceId: string]; toggle: [] }>();
const video = ref<HTMLVideoElement>(),
  overlay = ref<HTMLCanvasElement>(),
  active = ref(false),
  aspect = ref("16 / 9"),
  error = ref(""),
  devices = ref<MediaDeviceInfo[]>([]),
  device = ref(""),
  frames = ref(0),
  motion = ref(0),
  fps = ref(0),
  starting = ref(false);
let stream: MediaStream | undefined,
  timer: ReturnType<typeof setInterval> | undefined,
  previous: Uint8ClampedArray | undefined,
  lastTime = 0;
const buffer = document.createElement("canvas");
buffer.width = 160;
buffer.height = 90;
const ctx = buffer.getContext("2d", { willReadFrequently: true })!;
// Device stored for this camera in source_ref, as webcam:<deviceId>.
const assigned = computed(() =>
  props.camera.source_ref.startsWith("webcam:")
    ? props.camera.source_ref.slice(7)
    : "",
);
const busyIds = computed(() => props.taken ?? []);
const options = computed(() =>
  devices.value.filter(
    (d) => d.deviceId === device.value || !busyIds.value.includes(d.deviceId),
  ),
);
// Detected devices exist, but every one already belongs to another camera.
const allTaken = computed(
  () =>
    props.configurable && devices.value.length > 0 && !options.value.length,
);
const needsDevice = computed(() => !props.configurable && !assigned.value);
device.value = assigned.value;
watch(assigned, (v) => {
  if (!active.value && !starting.value) device.value = v;
});
async function refreshDevices() {
  if (!navigator.mediaDevices?.enumerateDevices) return;
  try {
    devices.value = (await navigator.mediaDevices.enumerateDevices()).filter(
      (d) => d.kind === "videoinput" && d.deviceId,
    );
  } catch {
    devices.value = [];
  }
}
function stop() {
  clearInterval(timer);
  stream?.getTracks().forEach((t) => t.stop());
  stream = undefined;
  if (video.value) {
    video.value.pause();
    video.value.srcObject = null;
    video.value.removeAttribute("src");
    video.value.load();
  }
  active.value = false;
  previous = undefined;
  overlay.value?.getContext("2d")?.clearRect(0, 0, 160, 90);
}
function trackDevice() {
  return stream?.getVideoTracks()[0]?.getSettings().deviceId ?? "";
}
async function start() {
  stop();
  starting.value = true;
  error.value = "";
  try {
    if (!navigator.mediaDevices?.getUserMedia)
      throw Error(
        "Abre esta aplicación en localhost o HTTPS para usar la cámara USB.",
      );
    if (needsDevice.value)
      throw Error(
        "Esta cámara todavía no tiene un dispositivo asignado. Hazlo en Cámaras y zonas.",
      );
    if (
      props.camera.source_ref === "webcam" ||
      props.camera.source_ref.startsWith("webcam:")
    ) {
      const chosen = device.value;
      stream = await navigator.mediaDevices.getUserMedia({
        video: chosen ? { deviceId: { exact: chosen } } : true,
        audio: false,
      });
      await refreshDevices();
      if (!chosen) {
        // Prefer a USB/UVC device that no other camera holds.
        const free = devices.value.filter(
          (d) => !busyIds.value.includes(d.deviceId),
        );
        const pick = free.find((d) => /usb|uvc/i.test(d.label)) ?? free[0];
        if (pick && pick.deviceId !== trackDevice()) {
          stream.getTracks().forEach((t) => t.stop());
          stream = await navigator.mediaDevices.getUserMedia({
            video: { deviceId: { exact: pick.deviceId } },
            audio: false,
          });
        }
      }
      video.value!.srcObject = stream;
      stream.getVideoTracks().forEach((t) =>
        t.addEventListener("ended", () => {
          stop();
          error.value =
            "La cámara se desconectó. Conéctala y vuelve a activarla.";
        }),
      );
    } else {
      throw Error(
        "Configura la fuente como webcam para usar la cámara USB. RTSP requiere un puente de video aún no conectado.",
      );
    }
    await video.value!.play();
    const w = video.value!.videoWidth,
      h = video.value!.videoHeight;
    if (w > 0 && h > 0) aspect.value = `${w} / ${h}`;
    active.value = true;
    frames.value = 0;
    lastTime = performance.now();
    timer = setInterval(processFrame, 150);
    device.value = trackDevice() || device.value;
    // Hand the device up so it is stored and no other camera offers it.
    if (props.configurable && device.value !== assigned.value)
      emit("assign", device.value);
  } catch (e) {
    stop();
    await refreshDevices();
    const name = e instanceof DOMException ? e.name : "";
    error.value =
      name === "NotAllowedError"
        ? "Permiso denegado. Habilita la cámara en el navegador y vuelve a intentarlo."
        : name === "OverconstrainedError" || name === "NotFoundError"
          ? "El dispositivo asignado a esta cámara ya no está disponible. Conéctalo o elige otro."
          : (e as Error).message;
  } finally {
    starting.value = false;
  }
}
async function choose() {
  if (!device.value) emit("assign", "");
  await start();
}
function processFrame() {
  if (!video.value || video.value.readyState < 2) return;
  ctx.drawImage(video.value, 0, 0, 160, 90);
  const data = ctx.getImageData(0, 0, 160, 90).data;
  let changed = 0,
    minX = 160,
    minY = 90,
    maxX = 0,
    maxY = 0;
  if (previous) {
    for (let y = 0; y < 90; y++)
      for (let x = 0; x < 160; x++) {
        const i = (y * 160 + x) * 4;
        if (
          (Math.abs(data[i]! - previous[i]!) +
            Math.abs(data[i + 1]! - previous[i + 1]!) +
            Math.abs(data[i + 2]! - previous[i + 2]!)) /
            3 >
          28
        ) {
          changed++;
          minX = Math.min(minX, x);
          maxX = Math.max(maxX, x);
          minY = Math.min(minY, y);
          maxY = Math.max(maxY, y);
        }
      }
  }
  previous = new Uint8ClampedArray(data);
  motion.value = (changed / 14400) * 100;
  frames.value++;
  const now = performance.now();
  fps.value = 1000 / (now - lastTime);
  lastTime = now;
  const c = overlay.value?.getContext("2d");
  if (c) {
    c.clearRect(0, 0, 160, 90);
    if (changed > 100) {
      c.strokeStyle = "#40e1a5";
      c.lineWidth = 1;
      c.strokeRect(minX, minY, maxX - minX, maxY - minY);
    }
  }
}
onMounted(() => {
  if (props.configurable) refreshDevices();
  navigator.mediaDevices?.addEventListener("devicechange", refreshDevices);
});
onUnmounted(() => {
  stop();
  navigator.mediaDevices?.removeEventListener("devicechange", refreshDevices);
});
defineExpose({ stop });
</script>
<template>
  <article class="camera-feed" :class="{ 'is-expanded': expanded }">
    <div class="feed-header">
      <b>{{ camera.name }}</b>
      <span class="feed-header-right">
        <span :class="active ? 'good' : 'muted'">{{
          active ? "● En vivo" : "Sin activar"
        }}</span>
        <button
          v-if="expandable"
          class="icon-button"
          @click="emit('toggle')"
          :aria-label="expanded ? 'Cerrar vista ampliada' : 'Ampliar cámara'"
        >
          {{ expanded ? "✕" : "⤢" }}
        </button>
      </span>
    </div>
    <div class="video-stage" :style="{ aspectRatio: aspect }">
      <video ref="video" muted playsinline></video
      ><canvas ref="overlay" width="160" height="90"></canvas>
      <p v-if="!active">
        {{ needsDevice ? "Sin dispositivo asignado" : "Cámara USB / portátil" }}
        <br /><small>{{
          needsDevice
            ? "Elige el dispositivo en Cámaras y zonas."
            : "Actívala para ver video y procesar movimiento."
        }}</small>
      </p>
    </div>
    <div class="feed-controls">
      <button
        v-if="!active"
        @click="start"
        :disabled="starting || allTaken || needsDevice"
      >
        {{ starting ? "Solicitando acceso…" : "Activar cámara" }}</button
      ><button v-else @click="stop">Detener</button
      ><select
        v-if="configurable && options.length"
        v-model="device"
        @change="choose"
        aria-label="Dispositivo de video"
      >
        <option value="">Predeterminada</option>
        <option v-for="d in options" :key="d.deviceId" :value="d.deviceId">
          {{ d.label || "Cámara sin nombre" }}
        </option>
      </select>
    </div>
    <p v-if="allTaken" class="feed-note">
      Los dispositivos detectados ya están asignados a otras cámaras. Libera uno
      allí o conecta otra cámara.
    </p>
    <p v-else-if="configurable && !devices.length" class="feed-note">
      Activa la cámara una vez para que el navegador revele la lista de
      dispositivos.
    </p>
    <p v-else-if="configurable && assigned" class="feed-note">
      Dispositivo guardado en esta cámara; no se ofrece en las demás.
    </p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <div class="processing-stats">
      <span>{{ fps.toFixed(1) }} FPS</span><span>{{ frames }} frames</span
      ><span>{{ motion.toFixed(1) }}% movimiento</span>
    </div>
  </article>
</template>
