# Modelo en vivo (camara_telefono.py) en CPU, para un servidor sin GPU: servicio
# modelo-vivo de compose.yaml. En la laptop se corre directo con Python y la GPU.
FROM python:3.13-slim-bookworm
# OpenCV necesita libGL y glib aunque no abra ventanas.
RUN apt-get update && apt-get install -y --no-install-recommends libgl1 libglib2.0-0 \
    && rm -rf /var/lib/apt/lists/*
# torch de CPU: sin las librerías de CUDA (varios GB) que sin GPU no se usan.
RUN pip install --no-cache-dir --index-url https://download.pytorch.org/whl/cpu torch==2.14.0 torchvision==0.29.0
# Las mismas versiones que el Build (Modelo/exportacion.json).
RUN pip install --no-cache-dir ultralytics==8.4.163 transformers==5.17.0 onnxruntime==1.30.0 \
    opencv-python==5.0.0.93 numpy==2.5.2 pandas==3.0.6 scipy==1.18.1 pillow==12.3.0 \
    websockets==17.1 threadpoolctl==3.7.0 ipython
WORKDIR /app
COPY . .
# camara_telefono.py escribe la configuración de Ultralytics en Modelo/.runtime al arrancar.
RUN useradd -m -u 10001 app && mkdir -p Modelo/.runtime && chown -R app Modelo/.runtime
USER app
ENTRYPOINT ["python", "-u", "camara_telefono.py"]
