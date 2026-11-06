"""Exercise the real desktop process without user settings or provider calls."""
import http.client
import json
import os
import pathlib
import socket
import subprocess
import tempfile
import time

with tempfile.TemporaryDirectory(prefix="voice-type-smoke-") as directory:
    root = pathlib.Path(directory)
    env = os.environ.copy()
    runtime = env.get("XDG_RUNTIME_DIR", f"/run/user/{os.getuid()}")
    if env.get("WAYLAND_DISPLAY") and not env["WAYLAND_DISPLAY"].startswith("/"):
        env["WAYLAND_DISPLAY"] = runtime + "/" + env["WAYLAND_DISPLAY"]
    env.update(HOME=str(root), XDG_STATE_HOME=str(root / "state"),
               XDG_CONFIG_HOME=str(root / "config"), XDG_DATA_HOME=str(root / "data"),
               XDG_RUNTIME_DIR=str(root / "runtime"), OPENAI_API_KEY="", OPENROUTER_API_KEY="",
               XDG_CURRENT_DESKTOP="smoke-test", XDG_SESSION_TYPE="wayland", GTK_A11Y="none", NO_AT_BRIDGE="1")
    (root / "runtime").mkdir(mode=0o700)
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    (root / ".voice-type-config.jsonc").write_text(json.dumps({"provider": "openai", "port": port}))
    binary = pathlib.Path(__file__).resolve().parents[1] / "build/voice-type-app"
    # Isolate single-instance and tray D-Bus state as well as the filesystem.
    bus = subprocess.Popen(["dbus-daemon", "--session", "--nofork", "--print-address=1"], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
    env["DBUS_SESSION_BUS_ADDRESS"] = bus.stdout.readline().strip()

    def request(method, path):
        connection = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
        connection.request(method, path)
        response = connection.getresponse()
        data = json.loads(response.read())
        connection.close()
        return response.status, data

    with (root / "app.log").open("w+") as log:
        process = subprocess.Popen([str(binary)], env=env, stdout=log, stderr=log)
        try:
            for _ in range(100):
                if process.poll() is not None:
                    raise RuntimeError("desktop exited during startup")
                try:
                    code, health = request("GET", "/health")
                    break
                except OSError:
                    time.sleep(.1)
            else:
                raise RuntimeError("listener never started")
            assert code == 200 and health["state"] == "idle"
            assert not (root / "runtime/voice-type/desktop.sock").exists()
            # Verify Linux attributes the listening socket to the Wails process.
            listening = []
            for line in pathlib.Path("/proc/net/tcp").read_text().splitlines()[1:]:
                columns = line.split()
                if columns[1] == f"0100007F:{port:04X}" and columns[3] == "0A":
                    listening.append(columns[9])
            descriptors = []
            for descriptor in pathlib.Path(f"/proc/{process.pid}/fd").iterdir():
                try:
                    descriptors.append(os.readlink(descriptor))
                except FileNotFoundError:
                    pass
            assert any(f"socket:[{inode}]" in descriptors for inode in listening), "Wails does not own listener"
            children = pathlib.Path(f"/proc/{process.pid}/task/{process.pid}/children").read_text().split()
            assert not any(pathlib.Path(f"/proc/{pid}/comm").read_text().strip() == "voice-type" for pid in children)
            code, _ = request("POST", "/toggle")
            assert code == 503, "missing credentials must block recording"
            second = subprocess.run([str(binary)], env=env, capture_output=True, timeout=10)
            assert second.returncode == 0, second.stderr.decode()
            assert process.poll() is None
            # A shortcut helper reaches the same listener and reports missing setup.
            helper = subprocess.run([str(binary), "--shortcut-action", "transcription"], env=env, capture_output=True, timeout=10)
            assert helper.returncode != 0 and b"HTTP 503" in helper.stderr
            request("POST", "/exit")
            assert process.wait(timeout=10) == 0
            with socket.socket() as listener:
                listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                listener.bind(("127.0.0.1", port))
            # Native OS shutdown follows the same cleanup path as Quit.
            process = subprocess.Popen([str(binary)], env=env, stdout=log, stderr=log)
            for _ in range(100):
                if process.poll() is not None:
                    raise RuntimeError("desktop exited on relaunch")
                try:
                    request("GET", "/health")
                    break
                except OSError:
                    time.sleep(.1)
            else:
                raise RuntimeError("relaunch listener never started")
            # Allow Wails to finish installing its OS signal handler.
            time.sleep(1)
            process.terminate()
            assert process.wait(timeout=10) == 0
            log.flush()
            log.seek(0)
            assert log.read().count("DAEMON: shutting down") == 2
            # Background startup failures must enter the native error-dialog
            # event loop instead of silently exiting before a window can exist.
            with socket.socket() as occupied:
                occupied.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                occupied.bind(("127.0.0.1", port))
                occupied.listen()
                process = subprocess.Popen([str(binary), "--background"], env=env, stdout=log, stderr=log)
                diagnostics = root / ".voice-type-logs.jsonl"
                for _ in range(100):
                    if process.poll() is not None:
                        raise RuntimeError("background startup failed before displaying its error dialog")
                    records = [json.loads(row) for row in diagnostics.read_text().splitlines()] if diagnostics.exists() else []
                    if any(row.get("event") == "startup.failed" for row in records):
                        break
                    time.sleep(.1)
                else:
                    raise RuntimeError("startup failure was not logged")
                time.sleep(1)
                assert process.poll() is None, "fatal dialog did not keep the native app alive"
                process.terminate()
                assert process.wait(timeout=10) in (0, 1)
            print("Native smoke passed: Wails owns HTTP, no daemon/socket, shortcuts reach service, second launch reuses app, exit and SIGTERM clean up, background startup failure stays visible and logs diagnostics.")
        except Exception:
            log.flush()
            log.seek(0)
            print(log.read()[-6000:])
            raise
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
            bus.terminate()
            bus.wait(timeout=5)
