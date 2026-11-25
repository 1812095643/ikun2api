"""Temporary loopback TCP bridge for a supervised gateway restart.

Only backend connection establishment is retried. No application bytes are
replayed, parsed, logged or stored. Remove the bridge after nginx is restored
and existing connections have drained.
"""
import argparse
import asyncio
import json
import os
from pathlib import Path
import signal
import time


class RestartBridge:
    def __init__(self, upstream_port, connect_wait=15.0, state_path=None, pause_path=None):
        self.upstream_port = upstream_port
        self.connect_wait = connect_wait
        self.state_path = Path(state_path) if state_path else None
        self.pause_path = Path(pause_path) if pause_path else None
        self.tasks = set()
        self.server = None
        self.accepted = 0
        self.connected = 0
        self.waiting = 0
        self.failed = 0
        self.draining = False

    def state(self):
        value = dict(active=len(self.tasks), waiting=self.waiting,
                     accepted=self.accepted, connected=self.connected,
                     failed=self.failed, draining=self.draining)
        if self.state_path:
            temp = self.state_path.with_suffix(".tmp")
            temp.write_text(json.dumps(value))
            os.chmod(temp, 0o600)
            temp.replace(self.state_path)
        return value

    async def start(self, port=0):
        self.server = await asyncio.start_server(self.handle, "127.0.0.1", port,
                                                  limit=65536, backlog=256)
        self.state()
        return self.server.sockets[0].getsockname()[1]

    async def drain(self):
        self.draining = True
        if self.server:
            self.server.close()
            await self.server.wait_closed()
        self.state()
        while self.tasks:
            await asyncio.gather(*list(self.tasks), return_exceptions=True)
        self.state()

    @staticmethod
    async def pump(reader, writer):
        while True:
            data = await reader.read(65536)
            if not data:
                if writer.can_write_eof():
                    writer.write_eof()
                    await writer.drain()
                return
            writer.write(data)
            await writer.drain()

    async def handle(self, reader, writer):
        task = asyncio.current_task()
        self.tasks.add(task)
        self.accepted += 1
        self.waiting += 1
        self.state()
        backend = None
        pumps = []
        waiting = True
        try:
            deadline = time.monotonic() + self.connect_wait
            while True:
                if time.monotonic() >= deadline:
                    self.failed += 1
                    return
                if self.pause_path and self.pause_path.exists():
                    await asyncio.sleep(.05)
                    continue
                try:
                    upstream, backend = await asyncio.wait_for(
                        asyncio.open_connection("127.0.0.1", self.upstream_port,
                                                limit=65536), timeout=.3)
                    break
                except (OSError, asyncio.TimeoutError):
                    if time.monotonic() >= deadline:
                        self.failed += 1
                        return
                    await asyncio.sleep(.05)
            # Retry ends here permanently, before any bytes are forwarded.
            self.waiting -= 1
            waiting = False
            self.connected += 1
            self.state()
            pumps = [asyncio.create_task(self.pump(reader, backend)),
                     asyncio.create_task(self.pump(upstream, writer))]
            await asyncio.gather(*pumps)
        except (OSError, asyncio.TimeoutError):
            pass
        finally:
            for pump in pumps:
                if not pump.done():
                    pump.cancel()
            if pumps:
                await asyncio.gather(*pumps, return_exceptions=True)
            for stream in (backend, writer):
                if stream:
                    stream.close()
                    try:
                        await asyncio.wait_for(stream.wait_closed(), 1)
                    except (OSError, asyncio.TimeoutError):
                        pass
            if waiting:
                self.waiting -= 1
            self.tasks.discard(task)
            self.state()


async def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--listen-port", type=int, default=18080)
    parser.add_argument("--upstream-port", type=int, default=8080)
    parser.add_argument("--state-file", required=True)
    parser.add_argument("--pause-file")
    args = parser.parse_args()
    bridge = RestartBridge(args.upstream_port, state_path=args.state_file, pause_path=args.pause_file)
    await bridge.start(args.listen_port)
    stopping = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGTERM, signal.SIGINT):
        loop.add_signal_handler(sig, stopping.set)
    await stopping.wait()
    await bridge.drain()


if __name__ == "__main__":
    asyncio.run(main())
