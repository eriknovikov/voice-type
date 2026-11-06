"""Exercise the packaged desktop without user settings or paid provider calls."""
import errno
import fcntl
import json
import os
import pathlib
import shutil
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
               GSETTINGS_BACKEND="keyfile", XDG_CURRENT_DESKTOP="smoke-test",
               XDG_SESSION_TYPE="wayland", GTK_A11Y="none", NO_AT_BRIDGE="1")
    (root / "runtime").mkdir(mode=0o700)
    binary = pathlib.Path(__file__).resolve().parents[1] / "build/voice-type"
    bus = subprocess.Popen(["dbus-daemon", "--session", "--nofork", "--print-address=1"],
                           stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, env=env)
    env["DBUS_SESSION_BUS_ADDRESS"] = bus.stdout.readline().strip()
    lock_path = root / "runtime/voice-type/engine.lock"
    diagnostics = root / ".voice-type-logs.jsonl"

    def invoke(*args):
        result = subprocess.run([str(binary), *args], env=env, capture_output=True, timeout=15)
        assert result.returncode == 0, result.stderr.decode()[-1000:]

    def records():
        if not diagnostics.exists():
            return []
        # An append may be in progress while inspecting diagnostics.
        return [json.loads(row) for row in diagnostics.read_bytes().split(b"\n")[:-1]]

    def owns_engine():
        if not lock_path.exists():
            return False
        with lock_path.open("r+") as lock:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                return False
            except BlockingIOError:
                return True

    def wait_for(check, message):
        for _ in range(150):
            if process.poll() is not None:
                raise RuntimeError("desktop exited during " + message)
            if check():
                return
            time.sleep(.1)
        raise RuntimeError(message)

    def no_listeners():
        descriptors = set()
        for fd in pathlib.Path(f"/proc/{process.pid}/fd").iterdir():
            try:
                descriptors.add(os.readlink(fd))
            except FileNotFoundError:
                pass
        for table in ("tcp", "tcp6"):
            for line in pathlib.Path("/proc/net/" + table).read_text().splitlines()[1:]:
                columns = line.split()
                assert columns[3] != "0A" or f"socket:[{columns[9]}]" not in descriptors, "desktop opened TCP listener"
        assert not (root / "runtime/voice-type/desktop.sock").exists()

    process = None
    occupied = socket.socket()
    try:
        occupied.bind(("127.0.0.1", 3232))
        occupied.listen()
    except OSError as error:
        if error.errno != errno.EADDRINUSE:
            raise
        occupied.close()
        occupied = None  # An already-running process occupies the default port.
    port = 3232
    with (root / "app.log").open("w+") as log:
        try:
            invoke("--quit")
            assert not lock_path.exists() and not diagnostics.exists()
            assert not (root / "state/voice-type").exists(), "cold quit wrote preferences"
            invalid = subprocess.run([str(binary), "--toggle", "--quit"], env=env, capture_output=True, timeout=10)
            assert invalid.returncode == 2 and not lock_path.exists()
            config = root / ".voice-type-config.jsonc"
            config.write_text(json.dumps({"port": port}))
            # Cold toggle with incomplete setup opens settings without recording.
            process = subprocess.Popen([str(binary), "--toggle"], env=env, stdout=log, stderr=log)
            wait_for(owns_engine, "engine ownership")
            wait_for(lambda: any(row["event"] == "desktop.started" for row in records()), "application startup")
            for args in (("--background",), ()):
                invoke(*args)
                assert owns_engine() and process.poll() is None
            assert not any(row["event"] == "dictation.completed" for row in records()), "cold toggle bypassed setup"
            no_listeners()
            # Each one-way command reaches the primary and fails safely on setup.
            for args in (("--toggle",), ("--toggle",)):
                previous = sum(row["event"] == "dictation.completed" for row in records())
                invoke(*args)
                wait_for(lambda: sum(row["event"] == "dictation.completed" for row in records()) > previous,
                         "forwarded toggle")
            assert all(row["outcome"] == "configuration_error" for row in records() if row["event"] == "dictation.completed")
            # Secondary launches must not read config/preferences or open logs.
            before = {path: path.read_bytes() for path in root.rglob("*") if path.is_file() and path.suffix in (".json", ".jsonc")}
            invoke("--background")
            assert all(path.read_bytes() == data for path, data in before.items())
            invoke("--quit")
            assert process.wait(timeout=15) == 0
            assert not owns_engine()
            # SIGTERM uses the same resource cleanup path; restart gets the lock.
            config.write_text("{broken")
            prior_attempts = sum(row["event"] == "dictation.completed" for row in records())
            process = subprocess.Popen([str(binary), "--toggle"], env=env, stdout=log, stderr=log)
            wait_for(owns_engine, "relaunch")
            wait_for(lambda: any(row["event"] == "configuration.recovered" for row in records()), "configuration recovery")
            invoke("--background")
            assert sum(row["event"] == "dictation.completed" for row in records()) == prior_attempts
            time.sleep(1)  # Wails must install its OS signal handler.
            process.terminate()
            assert process.wait(timeout=15) == 0 and not owns_engine()
            # Fatal lock conflict enters the native error dialog, without logs/config edits.
            with lock_path.open("r+") as lock:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                snapshot = diagnostics.read_bytes()
                process = subprocess.Popen([str(binary), "--background"], env=env, stdout=log, stderr=log)
                wait_for(lambda: "Voice Type startup failed:" in (root / "app.log").read_text(), "visible startup failure")
                time.sleep(1)
                assert process.poll() is None, "fatal dialog exited silently"
                assert diagnostics.read_bytes() == snapshot, "conflicting engine touched logs"
                process.terminate()
                assert process.wait(timeout=15) in (0, 1)
            print("Native smoke passed: no TCP/socket, occupied port ignored, cold quit/setup safe, single-instance commands work, Quit/SIGTERM release engine, startup conflict stays visible.")
        except Exception:
            log.flush()
            log.seek(0)
            print(log.read()[-6000:])
            raise
        finally:
            if process and process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
            if occupied:
                occupied.close()
            bus.terminate()
            bus.wait(timeout=5)
            if shutil.which("fusermount3"):
                for mount in ("doc", "gvfs"):
                    subprocess.run(["fusermount3", "-uz", str(root / "runtime" / mount)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
