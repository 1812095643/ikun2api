import asyncio
import socket
import tempfile
from pathlib import Path
import unittest
from restart_bridge import RestartBridge


class BridgeTests(unittest.IsolatedAsyncioTestCase):
    async def asyncTearDown(self):
        if hasattr(self, "server"):
            self.server.close()
            await self.server.wait_closed()
        if hasattr(self, "bridge"):
            await asyncio.wait_for(self.bridge.drain(), 3)

    async def test_waits_for_restart_and_preserves_bytes_and_half_close(self):
        sock = socket.socket()
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
        sock.close()
        self.bridge = RestartBridge(port, connect_wait=2)
        proxy = await self.bridge.start()
        reader, writer = await asyncio.open_connection("127.0.0.1", proxy)
        payload = bytes(range(256)) * 1024
        writer.write(payload)
        await writer.drain()
        writer.write_eof()
        await asyncio.sleep(.15)
        self.assertEqual(self.bridge.connected, 0)
        received = []

        async def serve(r, w):
            received.append(await r.read())
            for chunk in [b"data: first\n\n", b"data: last\n\n"]:
                w.write(chunk)
                await w.drain()
                await asyncio.sleep(.02)
            w.close()
            await w.wait_closed()

        self.server = await asyncio.start_server(serve, "127.0.0.1", port)
        result = await asyncio.wait_for(reader.read(), 2)
        self.assertEqual(result, b"data: first\n\ndata: last\n\n")
        self.assertEqual(received, [payload])
        writer.close()
        await writer.wait_closed()
        self.assertEqual(self.bridge.connected, 1)

    async def test_never_replays_after_connected_backend_closes(self):
        calls = []

        async def serve(r, w):
            calls.append(await r.read(10))
            w.close()
            await w.wait_closed()

        self.server = await asyncio.start_server(serve, "127.0.0.1", 0)
        self.bridge = RestartBridge(self.server.sockets[0].getsockname()[1])
        proxy = await self.bridge.start()
        reader, writer = await asyncio.open_connection("127.0.0.1", proxy)
        writer.write(b"one-request")
        await writer.drain()
        writer.write_eof()
        self.assertEqual(await asyncio.wait_for(reader.read(), 2), b"")
        await asyncio.sleep(.1)
        self.assertEqual(len(calls), 1)
        writer.close()
        await writer.wait_closed()

    async def test_unavailable_backend_has_bounded_wait(self):
        sock = socket.socket()
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
        sock.close()
        self.bridge = RestartBridge(port, connect_wait=.1)
        proxy = await self.bridge.start()
        reader, writer = await asyncio.open_connection("127.0.0.1", proxy)
        self.assertEqual(await asyncio.wait_for(reader.read(), 2), b"")
        writer.close()
        await writer.wait_closed()
        self.assertEqual(self.bridge.failed, 1)

    async def test_pause_gate_and_graceful_drain(self):
        async def serve(r, w):
            data = await r.read()
            w.write(data)
            await w.drain()
            w.close()
            await w.wait_closed()

        self.server = await asyncio.start_server(serve, "127.0.0.1", 0)
        with tempfile.TemporaryDirectory() as directory:
            pause = Path(directory) / "pause"
            pause.touch()
            self.bridge = RestartBridge(self.server.sockets[0].getsockname()[1], pause_path=pause)
            proxy = await self.bridge.start()
            reader, writer = await asyncio.open_connection("127.0.0.1", proxy)
            writer.write(b"preserved-stream")
            await writer.drain()
            await asyncio.sleep(.1)
            self.assertEqual(self.bridge.connected, 0)
            pause.unlink()
            for _ in range(20):
                if self.bridge.connected:
                    break
                await asyncio.sleep(.02)
            draining = asyncio.create_task(self.bridge.drain())
            await asyncio.sleep(.05)
            self.assertFalse(draining.done(), "drain must retain the active stream")
            writer.write_eof()
            self.assertEqual(await asyncio.wait_for(reader.read(), 2), b"preserved-stream")
            writer.close()
            await writer.wait_closed()
            await asyncio.wait_for(draining, 2)


if __name__ == "__main__":
    unittest.main()
