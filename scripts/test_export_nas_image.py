"""Regression checks for the Docker-save delivery format and runtime isolation."""

import contextlib
import hashlib
import importlib.util
import io
import json
import tarfile
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace

spec = importlib.util.spec_from_file_location("export_nas_image", Path(__file__).with_name("export-nas-image.py"))
exporter = importlib.util.module_from_spec(spec)
spec.loader.exec_module(exporter)


class ImageExportTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="remlink-image-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.context = self.root / "release"
        (self.context / "linux-amd64").mkdir(parents=True)
        (self.context / "docker").mkdir()
        self.binary = b"\x7fELF\x02\x01" + bytes(12) + b"\x3e\x00" + b"current-server"
        (self.context / "linux-amd64/remlink-server").write_bytes(self.binary)
        (self.context / "linux-amd64/THIRD_PARTY_NOTICES.md").write_bytes(b"licenses")
        (self.context / "docker/preflight.sh").write_bytes(b"#!/bin/sh\nexec \"$@\"\n")
        (self.context / "image.txt").write_text("remlink/server:1.1.0\n", encoding="utf-8")
        checksums = [f"{exporter.digest_file(p)}  {p.relative_to(self.context).as_posix()}" for p in self.context.rglob("*") if p.is_file()]
        (self.context / "SHA256SUMS.txt").write_text("\n".join(checksums) + "\n", encoding="ascii")
        self.runtime = self.root / "old-image.tar.gz"
        self.output = self.root / "remlink-server-1.1.0-amd64.tar.gz"
        self.layers = [
            self.layer({"bin/sh": b"shell", "etc/ssl/certs/ca-certificates.crt": b"certificates"}),
            self.layer({"usr/bin/ip": b"ip", "usr/bin/wg": b"wg", "usr/sbin/iptables": b"iptables"}),
            self.layer({"usr/local/bin/remlink-server": b"old-server", "app/data/remlink.db": b"private-old-data"}),
        ]
        self.config = {
            "os": "linux", "architecture": "amd64",
            "rootfs": {"type": "layers", "diff_ids": ["sha256:" + hashlib.sha256(layer).hexdigest() for layer in self.layers]},
            "history": [{"created_by": "Debian"}, {"created_by": "RUN install wireguard-tools"}, {"created_by": "COPY remlink-server"}],
            "config": {
                "Entrypoint": ["/usr/local/bin/remlink-preflight"],
                "Cmd": ["/usr/local/bin/remlink-server", "-config", "/etc/remlink/server.yaml"],
                "Healthcheck": {"Test": ["CMD", "/usr/local/bin/remlink-server", "-config", "/etc/remlink/server.yaml", "-healthcheck"]},
                "Volumes": {"/app/data": {}},
                "Labels": {"org.opencontainers.image.version": "1.0.8"},
            },
        }
        self.write_runtime()

    @staticmethod
    def layer(files):
        data = io.BytesIO()
        with tarfile.open(fileobj=data, mode="w") as archive:
            for name, content in files.items():
                exporter.add_bytes(archive, name, content)
        return data.getvalue()

    def write_runtime(self):
        config_data = exporter.json_bytes(self.config)
        config_name = hashlib.sha256(config_data).hexdigest() + ".json"
        names = [hashlib.sha256(layer).hexdigest() + "/layer.tar" for layer in self.layers]
        with tarfile.open(self.runtime, "w:gz") as archive:
            exporter.add_bytes(archive, config_name, config_data)
            exporter.add_bytes(archive, "manifest.json", exporter.json_bytes([{"Config": config_name, "RepoTags": ["remlink/server:1.0.8"], "Layers": names}]))
            for name, layer in zip(names, self.layers):
                exporter.add_bytes(archive, name, layer)

    def export(self):
        with contextlib.redirect_stdout(io.StringIO()):
            exporter.export(SimpleNamespace(context=self.context, runtime_image=self.runtime, output=self.output))

    def test_docker_save_format_replaces_old_application_and_excludes_state(self):
        self.export()
        with tarfile.open(self.output, "r:gz") as archive:
            manifest = json.load(archive.extractfile("manifest.json"))[0]
            self.assertEqual(manifest["RepoTags"], ["remlink/server:1.1.0"])
            config = json.load(archive.extractfile(manifest["Config"]))
            self.assertEqual(config["config"]["Labels"]["org.opencontainers.image.version"], "1.1.0")
            self.assertEqual(config["config"]["Volumes"], {"/app/data": {}})
            self.assertEqual(config["rootfs"]["diff_ids"][:2], self.config["rootfs"]["diff_ids"][:2])
            for name, expected in zip(manifest["Layers"][:2], self.layers[:2]):
                self.assertEqual(archive.extractfile(name).read(), expected)
            with tarfile.open(fileobj=archive.extractfile(manifest["Layers"][-1])) as application:
                self.assertEqual(application.extractfile("usr/local/bin/remlink-server").read(), self.binary)
                self.assertEqual(application.getmember("usr/local/bin/remlink-server").mode, 0o755)
                self.assertEqual(application.getmember("usr/local/bin/remlink-preflight").mode, 0o755)
                self.assertNotIn("app/data/remlink.db", application.getnames())
                self.assertNotIn("etc/remlink/server.yaml", application.getnames())
        checksum = Path(str(self.output) + ".sha256").read_text().split()[0]
        self.assertEqual(checksum, exporter.digest_file(self.output))
        with self.assertRaisesRegex(ValueError, "Output already exists"):
            self.export()

    def test_previously_exported_image_can_supply_runtime_layers(self):
        self.config["history"][-1]["created_by"] = "COPY current RemLink Server release, preflight and notices; mkdir /app/data"
        self.write_runtime()
        self.export()
        self.assertTrue(self.output.is_file())

    def test_tampered_release_fails_before_publishing_image(self):
        (self.context / "linux-amd64/remlink-server").write_bytes(b"tampered")
        with self.assertRaisesRegex(ValueError, "Release checksum mismatch"):
            self.export()
        self.assertFalse(self.output.exists())

    def test_tampered_runtime_layer_fails_before_publishing_image(self):
        self.config["rootfs"]["diff_ids"][0] = "sha256:" + "0" * 64
        self.write_runtime()
        with self.assertRaisesRegex(ValueError, "Runtime layer digest mismatch"):
            self.export()
        self.assertFalse(self.output.exists())

    def test_wrong_runtime_platform_is_rejected(self):
        self.config["architecture"] = "arm64"
        self.write_runtime()
        with self.assertRaisesRegex(ValueError, "Runtime must be linux/amd64"):
            self.export()


if __name__ == "__main__":
    unittest.main()
