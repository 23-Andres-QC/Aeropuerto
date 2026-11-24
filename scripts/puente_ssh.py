"""Puente WebSocket -> SSH del servidor, para el modelo de la laptop en redes que bloquean el puerto 22 (las de ESAN).

Corre en el servidor (puente-ssh de compose.yaml, perfil servidor) y escucha solo en 127.0.0.1:PUENTE_PUERTO; el nginx
del sistema le pasa wss://aeropuerto.tujuegoya.com/tunel-ssh (servidor.nginx.conf). Cada WebSocket que trae la clave
TUNEL_CLAVE (la del .env del servidor y de la laptop) se une al SSH de este servidor (127.0.0.1:22): por dentro va la
misma conexión SSH de scripts/modelo_local.py, con la contraseña de siempre. Sin TUNEL_CLAVE no deja pasar a nadie.
Solo la biblioteca estándar de Python.
"""
import asyncio
import base64
import hashlib
import hmac
import os
import struct

CLAVE = os.environ.get("TUNEL_CLAVE", "")
PUERTO = int(os.environ.get("PUENTE_PUERTO") or 8095)
SSH = ("127.0.0.1", 22)
GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"  # el del protocolo WebSocket (RFC 6455)
MAX_MARCO = 1 << 20  # los marcos del modelo son de hasta 64 KB


def desenmascarar(datos, mascara):
    n = len(datos)
    if not n:
        return b""
    m = (mascara * (n // 4 + 1))[:n]
    return (int.from_bytes(datos, "big") ^ int.from_bytes(m, "big")).to_bytes(n, "big")


def marco(opcode, datos):
    """Un marco del servidor al cliente (sin máscara)."""
    n = len(datos)
    if n < 126:
        cabecera = struct.pack(">BB", 0x80 | opcode, n)
    elif n < 1 << 16:
        cabecera = struct.pack(">BBH", 0x80 | opcode, 126, n)
    else:
        cabecera = struct.pack(">BBQ", 0x80 | opcode, 127, n)
    return cabecera + datos


async def responder(escritor, estado):
    escritor.write(f"HTTP/1.1 {estado}\r\nContent-Length: 0\r\nConnection: close\r\n\r\n".encode())
    try:
        await escritor.drain()
    except ConnectionError:
        pass
    escritor.close()


async def ws_a_ssh(lector, escritor, ssh):
    try:
        while True:
            b0, b1 = await lector.readexactly(2)
            opcode, largo = b0 & 0x0F, b1 & 0x7F
            if largo == 126:
                largo = struct.unpack(">H", await lector.readexactly(2))[0]
            elif largo == 127:
                largo = struct.unpack(">Q", await lector.readexactly(8))[0]
            if largo > MAX_MARCO or not b1 & 0x80:  # los marcos del cliente van siempre con máscara
                return
            mascara = await lector.readexactly(4)
            datos = desenmascarar(await lector.readexactly(largo), mascara)
            if opcode in (0, 2):
                ssh.write(datos)
                await ssh.drain()
            elif opcode == 8:
                return
            elif opcode == 9:
                escritor.write(marco(10, datos))
                await escritor.drain()
    except (asyncio.IncompleteReadError, ConnectionError):
        return


async def ssh_a_ws(ssh, escritor):
    try:
        while True:
            datos = await ssh.read(65536)
            if not datos:
                return
            escritor.write(marco(2, datos))
            await escritor.drain()
    except ConnectionError:
        return


async def atender(lector, escritor):
    try:
        pedido = await asyncio.wait_for(lector.readuntil(b"\r\n\r\n"), 10)
    except (asyncio.IncompleteReadError, asyncio.LimitOverrunError, asyncio.TimeoutError, ConnectionError):
        escritor.close()
        return
    campos = {}
    for linea in pedido.decode("latin-1").split("\r\n")[1:]:
        nombre, _, valor = linea.partition(":")
        campos[nombre.strip().lower()] = valor.strip()
    ip = campos.get("x-real-ip", "?")
    if not CLAVE:
        await responder(escritor, "503 Service Unavailable")
        return
    if not hmac.compare_digest(campos.get("x-tunel-clave", "").encode(), CLAVE.encode()):
        print(f"{ip}: clave incorrecta", flush=True)
        await responder(escritor, "403 Forbidden")
        return
    llave = campos.get("sec-websocket-key", "")
    if campos.get("upgrade", "").lower() != "websocket" or not llave:
        await responder(escritor, "400 Bad Request")
        return
    try:
        ssh_lector, ssh_escritor = await asyncio.wait_for(asyncio.open_connection(*SSH), 10)
    except (OSError, asyncio.TimeoutError):
        await responder(escritor, "502 Bad Gateway")
        return
    acepta = base64.b64encode(hashlib.sha1((llave + GUID).encode()).digest()).decode()
    escritor.write(("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
                    f"Sec-WebSocket-Accept: {acepta}\r\n\r\n").encode())
    await escritor.drain()
    print(f"{ip}: túnel abierto", flush=True)
    tareas = {asyncio.create_task(ws_a_ssh(lector, escritor, ssh_escritor)),
              asyncio.create_task(ssh_a_ws(ssh_lector, escritor))}
    await asyncio.wait(tareas, return_when=asyncio.FIRST_COMPLETED)
    for tarea in tareas:
        tarea.cancel()
    ssh_escritor.close()
    escritor.close()
    print(f"{ip}: túnel cerrado", flush=True)


async def main():
    if not CLAVE:
        print("Sin TUNEL_CLAVE en el .env: el puente no deja pasar a nadie.", flush=True)
    servidor = await asyncio.start_server(atender, "127.0.0.1", PUERTO)
    print(f"Puente SSH en 127.0.0.1:{PUERTO} -> {SSH[0]}:{SSH[1]}", flush=True)
    async with servidor:
        await servidor.serve_forever()


if __name__ == "__main__":
    asyncio.run(main())
