#!/usr/bin/env python3
"""Export a tested release context using an existing RemLink runtime image.

Only the Debian and installed system-tool layers are retained. The old RemLink
application layers are discarded, and the current release files replace them.
The output uses Docker's manifest.json / <diff-id>/layer.tar archive layout.
"""

import argparse
import copy
import gzip
import hashlib
import io
import json
import re
import shutil
import struct
import tarfile
import tempfile
from datetime import datetime, timezone
from pathlib import Path


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest_file(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def json_bytes(value):
    return json.dumps(value, separators=(",", ":"), ensure_ascii=False).encode()


def add_bytes(archive, name, data, mode=0o644, mtime=0):
    entry = tarfile.TarInfo(name)
    entry.mode, entry.mtime, entry.size = mode, mtime, len(data)
    archive.addfile(entry, io.BytesIO(data))


def validate_archive(path, tag, expected_files):
    """Read the delivered archive back; verify config, every layer and payload."""
    with tarfile.open(path, "r:gz") as archive:
        manifest = json.load(archive.extractfile("manifest.json"))
        require(len(manifest) == 1 and manifest[0]["RepoTags"] == [tag], "Wrong image tag")
        image = manifest[0]
        config_data = archive.extractfile(image["Config"]).read()
        require(image["Config"] == hashlib.sha256(config_data).hexdigest() + ".json", "Config digest mismatch")
        config = json.loads(config_data)
        require((config["os"], config["architecture"]) == ("linux", "amd64"), "Wrong platform")
        require(len(image["Layers"]) == len(config["rootfs"]["diff_ids"]) == 3, "Expected two runtime layers and one application layer")
        for layer_name, diff_id in zip(image["Layers"], config["rootfs"]["diff_ids"]):
            actual = hashlib.file_digest(archive.extractfile(layer_name), "sha256").hexdigest()
            require("sha256:" + actual == diff_id, f"Layer digest mismatch: {layer_name}")
            require(layer_name == actual + "/layer.tar", "Unexpected layer layout")
        with tarfile.open(fileobj=archive.extractfile(image["Layers"][-1])) as application:
            require(set(application.getnames()) == set(expected_files) | {"app/data"}, "Unexpected application files")
            for name, (data, mode) in expected_files.items():
                require(application.extractfile(name).read() == data, f"Payload mismatch: {name}")
                require(application.getmember(name).mode == mode, f"Incorrect permissions: {name}")
    return hashlib.sha256(config_data).hexdigest()


def export(args):
    context = args.context.resolve()
    output = args.output.resolve()
    require(output.name.endswith(".tar.gz"), "Output must end in .tar.gz")
    require(not output.exists(), f"Output already exists: {output}")
    tag = (context / "image.txt").read_text(encoding="utf-8").strip()
    require(re.fullmatch(r"remlink/server:\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?", tag), "Invalid release image tag")
    version = tag.split(":", 1)[1]
    # Release contexts must pass the original build script's checksum manifest.
    for line in (context / "SHA256SUMS.txt").read_text(encoding="utf-8").splitlines():
        expected, relative = line.split("  ", 1)
        checked = (context / relative).resolve()
        require(checked.is_relative_to(context), "Checksum path escapes release context")
        require(digest_file(checked) == expected, f"Release checksum mismatch: {relative}")
    binary = (context / "linux-amd64/remlink-server").read_bytes()
    require(binary[:6] == b"\x7fELF\x02\x01" and struct.unpack_from("<H", binary, 18)[0] == 62, "Server must be a Linux amd64 ELF binary")
    preflight = (context / "docker/preflight.sh").read_bytes()
    require(preflight.startswith(b"#!/bin/sh\n") and b"\r" not in preflight, "Entrypoint must use LF line endings")
    payload = {
        "usr/local/bin/remlink-server": (binary, 0o755),
        "usr/local/bin/remlink-preflight": (preflight, 0o755),
        "usr/share/doc/remlink/THIRD_PARTY_NOTICES.md": ((context / "linux-amd64/THIRD_PARTY_NOTICES.md").read_bytes(), 0o644),
    }
    runtime_digest = digest_file(args.runtime_image)
    checksum = Path(str(args.runtime_image) + ".sha256")
    if checksum.exists():
        require(runtime_digest == checksum.read_text(encoding="utf-8-sig").split()[0], "Runtime archive checksum mismatch")
    output.parent.mkdir(parents=True, exist_ok=True)
    partial = output.with_name(output.name + ".partial")
    require(not partial.exists(), f"Partial output already exists: {partial}")
    with tempfile.TemporaryDirectory(prefix="remlink-nas-") as temporary:
        stage = Path(temporary)
        with tarfile.open(args.runtime_image, "r:*") as source:
            manifests = json.load(source.extractfile("manifest.json"))
            require(len(manifests) == 1 and any(t.startswith("remlink/server:") for t in manifests[0]["RepoTags"]), "Runtime must be a single RemLink Server image")
            manifest = manifests[0]
            original = json.load(source.extractfile(manifest["Config"]))
            require((original["os"], original["architecture"]) == ("linux", "amd64"), "Runtime must be linux/amd64")
            history = []
            for item in original["history"]:
                if "remlink-server" in item.get("created_by", ""):
                    break
                history.append(item)
            count = sum(not item.get("empty_layer", False) for item in history)
            require(count == 2 and "wireguard-tools" in history[-1].get("created_by", ""), "Runtime must have Debian and system-tool layers before the first application COPY")
            layers, diff_ids, runtime_files = [], [], set()
            for index, layer_name in enumerate(manifest["Layers"][:count]):
                destination = stage / f"runtime-{index}.tar"
                with source.extractfile(layer_name) as compressed:
                    signature = compressed.read(2)
                    compressed.seek(0)
                    stream = gzip.GzipFile(fileobj=compressed) if signature == b"\x1f\x8b" else compressed
                    with destination.open("wb") as target:
                        shutil.copyfileobj(stream, target)
                digest = digest_file(destination)
                require("sha256:" + digest == original["rootfs"]["diff_ids"][index], "Runtime layer digest mismatch")
                with tarfile.open(destination) as layer:
                    names = {m.name.removeprefix("./").rstrip("/") for m in layer.getmembers()}
                    require(not any("remlink" in name or name.startswith("app/data/") for name in names), "Runtime layers contain application state")
                    runtime_files.update(names)
                layers.append((digest + "/layer.tar", destination))
                diff_ids.append("sha256:" + digest)
        require({"usr/bin/ip", "usr/bin/wg", "usr/sbin/iptables", "etc/ssl/certs/ca-certificates.crt"} <= runtime_files, "Missing runtime tools or certificates")
        require("bin/sh" in runtime_files or {"bin", "usr/bin/sh"} <= runtime_files, "Missing runtime shell")
        created = datetime.now(timezone.utc)
        application = stage / "application.tar"
        with tarfile.open(application, "w", format=tarfile.PAX_FORMAT) as layer:
            for name, (data, mode) in payload.items():
                add_bytes(layer, name, data, mode, int(created.timestamp()))
            directory = tarfile.TarInfo("app/data")
            directory.type, directory.mode, directory.mtime = tarfile.DIRTYPE, 0o755, int(created.timestamp())
            layer.addfile(directory)
        application_digest = digest_file(application)
        layers.append((application_digest + "/layer.tar", application))
        diff_ids.append("sha256:" + application_digest)
        config = copy.deepcopy(original)
        config["created"] = created.isoformat().replace("+00:00", "Z")
        config["history"] = history + [{"created": config["created"], "created_by": "COPY current RemLink Server release, preflight and notices; mkdir /app/data"}]
        config["rootfs"] = {"type": "layers", "diff_ids": diff_ids}
        config["config"].pop("ArgsEscaped", None)
        require(config["config"]["Entrypoint"] == ["/usr/local/bin/remlink-preflight"], "Unexpected entrypoint")
        require(config["config"]["Cmd"] == ["/usr/local/bin/remlink-server", "-config", "/etc/remlink/server.yaml"], "Unexpected server command")
        require(config["config"]["Healthcheck"]["Test"] == ["CMD", "/usr/local/bin/remlink-server", "-config", "/etc/remlink/server.yaml", "-healthcheck"], "Unexpected healthcheck")
        config["config"].setdefault("Labels", {}).update({"org.opencontainers.image.version": version})
        config_data = json_bytes(config)
        config_name = hashlib.sha256(config_data).hexdigest() + ".json"
        manifest_data = json_bytes([{"Config": config_name, "RepoTags": [tag], "Layers": [name for name, _ in layers]}])
        try:
            with partial.open("xb") as raw, gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed, tarfile.open(fileobj=compressed, mode="w|") as archive:
                add_bytes(archive, config_name, config_data)
                add_bytes(archive, "manifest.json", manifest_data)
                for name, path in layers:
                    entry = tarfile.TarInfo(name)
                    entry.mode, entry.size = 0o644, path.stat().st_size
                    with path.open("rb") as stream:
                        archive.addfile(entry, stream)
            image_id = validate_archive(partial, tag, payload)
            partial.replace(output)
        except Exception:
            partial.unlink(missing_ok=True)
            raise
    sha256 = digest_file(output)
    Path(str(output) + ".sha256").write_text(f"{sha256}  {output.name}\n", encoding="ascii")
    print(json.dumps({"archive": str(output), "bytes": output.stat().st_size, "image": tag, "platform": "linux/amd64", "image_id": "sha256:" + image_id, "sha256": sha256, "runtime_archive_sha256": runtime_digest, "server_sha256": hashlib.sha256(binary).hexdigest(), "verified": "config and all layer digests, release checksums, exact binary and entrypoint, file permissions"}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runtime-image", type=Path, required=True, help="Existing Docker-save archive of RemLink Server with Debian runtime tools")
    parser.add_argument("--context", type=Path, required=True, help="Tested context from scripts/build-docker-package.ps1")
    parser.add_argument("--output", type=Path, required=True, help="Docker image .tar.gz to create")
    export(parser.parse_args())
