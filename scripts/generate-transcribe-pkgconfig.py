#!/usr/bin/env python3
"""Create the pkg-config link surface from transcribe.cpp's install manifest."""

import json
import pathlib
import sys


def main() -> None:
    if len(sys.argv) != 4:
        raise SystemExit("usage: generate-transcribe-pkgconfig.py PREFIX MANIFEST OUTDIR")
    prefix = pathlib.Path(sys.argv[1]).resolve()
    manifest_path = pathlib.Path(sys.argv[2])
    output_dir = pathlib.Path(sys.argv[3])
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if manifest.get("shared"):
        raise SystemExit("Voice Type requires the static transcribe.cpp link posture")
    if manifest.get("frameworks"):
        raise SystemExit("unexpected platform frameworks in the Linux link manifest")

    lib_dir = str(pathlib.PurePosixPath(manifest.get("lib_dir", "lib")))
    include_dir = str(pathlib.PurePosixPath(manifest.get("include_dir", "include")))
    libs = [f"-l{name}" for name in manifest.get("libraries", [])]
    for raw_path in manifest.get("library_paths", []):
        path = pathlib.Path(raw_path)
        name = path.name
        for suffix in (".a", ".dylib", ".lib"):
            if name.endswith(suffix):
                name = name[: -len(suffix)]
                if name.startswith("lib"):
                    name = name[3:]
                libs.extend((f"-L{path.parent}", f"-l{name}"))
                break
        else:
            libs.append(str(path))
    libs.extend(f"-l{name}" for name in manifest.get("system_libs", []))
    libs.extend(manifest.get("link_flags", []))
    if not libs:
        raise SystemExit("transcribe.cpp install manifest has no libraries")

    output_dir.mkdir(parents=True, exist_ok=True)
    package = output_dir / "voicetype-transcribe.pc"
    package.write_text(
        "\n".join(
            (
                f"prefix={prefix}",
                "exec_prefix=${prefix}",
                f"libdir=${{prefix}}/{lib_dir}",
                f"includedir=${{prefix}}/{include_dir}",
                "",
                "Name: voicetype-transcribe",
                "Description: Pinned static transcribe.cpp engine for Voice Type",
                f"Version: {manifest['version']}",
                "Libs: -L${libdir} " + " ".join(libs),
                "Cflags: -I${includedir}",
                "",
            )
        ),
        encoding="utf-8",
    )


if __name__ == "__main__":
    main()
