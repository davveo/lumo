"""Runtime source ownership tests; no Docker, Go or JS runtime is needed."""

from __future__ import annotations

from pathlib import Path
import subprocess
import tempfile
import unittest

PROJECT = Path(__file__).resolve().parents[1]


class SourceFingerprintTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="lumo fingerprint ")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name) / "project with spaces"
        self.root.mkdir()
        self.outside = Path(self.temporary.name) / "outside cwd"
        self.outside.mkdir()
        self.write("scripts/source-fingerprint.sh", (PROJECT / "scripts/source-fingerprint.sh").read_text())
        for name in ("order", "catalog", "inventory", "payment"):
            self.write(f"services/{name}/service.go", "package " + name + "\n")
            self.write(f"services/{name}/internal/usecase/service.go", "package usecase\n")
        for path in (
            "go.mod",
            "go.sum",
            "Dockerfile",
            ".dockerignore",
            "api/mall/v1/domain.proto",
            "api/mall/v1/domain.pb.go",
            "internal/platform/rpc.go",
            "internal/bootstrap/registry.go",
            "internal/democatalog/catalog.go",
            "internal/httpgateway/gateway.go",
            "cmd/server/main.go",
            "cmd/gateway/main.go",
            "web/dist/index.html",
            "web/Dockerfile",
            "web/.dockerignore",
            "web/nginx/default.conf.template",
            "web/nginx/proxy.conf",
            "web/dist/assets/item with spaces.jpg",
            "deploy/minio.Dockerfile",
            "deploy/observability.Dockerfile",
            "cmd/healthprobe/main.go",
        ):
            self.write(path, "initial " + path + "\n")

    def write(self, name: str, contents: str) -> None:
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(contents, encoding="utf-8")

    def digest(self, name: str) -> str:
        completed = subprocess.run(
            ["sh", str(self.root / "scripts/source-fingerprint.sh"), name],
            cwd=self.outside,
            text=True,
            capture_output=True,
            timeout=15,
        )
        self.assertEqual(completed.returncode, 0, completed.stderr)
        value = completed.stdout.strip()
        self.assertRegex(value, r"^[0-9a-f]{64}$")
        return value

    def all_digests(self) -> dict[str, str]:
        return {
            name: self.digest(name)
            for name in ("order", "catalog", "inventory", "payment", "gateway", "web", "--image", "--minio", "--observability")
        }

    def changed_by(self, name: str, contents: str) -> set[str]:
        before = self.all_digests()
        self.write(name, contents)
        return {service for service, value in self.all_digests().items() if value != before[service]}

    def test_a_domain_edit_updates_only_its_service_and_the_build_image(self) -> None:
        self.assertEqual(
            self.changed_by("services/order/internal/usecase/service.go", "package usecase\n// updated\n"),
            {"order", "--image"},
        )

    def test_frontend_changes_update_only_the_frontend_image(self) -> None:
        self.assertEqual(
            self.changed_by("web/dist/assets/item with spaces.jpg", "changed image bytes"), {"web"}
        )

    def test_frontend_recipe_and_proxy_configuration_have_only_one_owner(self) -> None:
        for path in ("web/Dockerfile", "web/.dockerignore", "web/nginx/default.conf.template", "web/nginx/proxy.conf"):
            with self.subTest(path=path):
                self.assertEqual(self.changed_by(path, "updated frontend serving configuration\n"), {"web"})

    def test_gateway_http_changes_do_not_affect_domain_service_sources(self) -> None:
        self.assertEqual(
            self.changed_by("internal/httpgateway/gateway.go", "package httpgateway\n"), {"gateway", "--image"}
        )

    def test_demo_catalog_has_two_owners(self) -> None:
        self.assertEqual(
            self.changed_by("internal/democatalog/catalog.go", "package democatalog\n"),
            {"catalog", "inventory", "--image"},
        )

    def test_common_platform_changes_affect_every_application(self) -> None:
        self.assertEqual(
            self.changed_by("internal/platform/rpc.go", "package platform\n"),
            {"order", "catalog", "inventory", "payment", "gateway", "--image"},
        )

    def test_server_entrypoint_changes_affect_domains_but_not_gateway(self) -> None:
        self.assertEqual(
            self.changed_by("cmd/server/main.go", "package main\n"),
            {"order", "catalog", "inventory", "payment", "--image"},
        )

    def test_documentation_tests_devstack_and_deployment_state_do_not_change_runtime(self) -> None:
        before = self.all_digests()
        for name in (
            "README.md",
            "services/order/README.md",
            "services/order/order_test.go",
            "services/order/testdata/fixture.json",
            "services/order/tests/service.go",
            "services/order/docs/design.go",
            "cmd/devstack/main.go",
            "internal/devstack/stack.go",
            "docs/design.md",
            "tests/test_other.py",
            ".local/start/order.state",
        ):
            self.write(name, "new development content\n")
        self.assertEqual(before, self.all_digests())

    def test_additions_deletions_and_renames_are_detected(self) -> None:
        before = self.digest("order")
        self.write("services/order/policy.go", "package order\n")
        added = self.digest("order")
        self.assertNotEqual(before, added)
        (self.root / "services/order/policy.go").rename(self.root / "services/order/renamed.go")
        self.assertNotEqual(added, self.digest("order"))
        (self.root / "services/order/renamed.go").unlink()
        self.assertEqual(before, self.digest("order"))

    def test_creation_order_and_modification_times_do_not_affect_fingerprint(self) -> None:
        self.write("services/order/z.go", "package order\n// z\n")
        self.write("services/order/a.go", "package order\n// a\n")
        first = self.digest("order")
        for name in ("z.go", "a.go"):
            path = self.root / "services/order" / name
            contents = path.read_text()
            path.unlink()
            path.write_text(contents)
        self.assertEqual(first, self.digest("order"))

    def test_unclassified_internal_packages_are_conservatively_shared(self) -> None:
        self.assertEqual(
            self.changed_by("internal/newshared/policy.go", "package newshared\n"),
            {"order", "catalog", "inventory", "payment", "gateway", "--image"},
        )

    def test_minio_recipe_does_not_redeploy_application_containers(self) -> None:
        self.assertEqual(self.changed_by("deploy/minio.Dockerfile", "FROM alpine:3.22.1\n"), {"--minio"})

    def test_observability_recipe_and_probe_only_affect_observability_images(self) -> None:
        for name in ("deploy/observability.Dockerfile", "cmd/healthprobe/main.go"):
            with self.subTest(name=name):
                self.assertEqual(self.changed_by(name, "changed observability source\n"), {"--observability"})

    def test_probe_tests_and_runtime_configuration_do_not_change_image_sources(self) -> None:
        before = self.all_digests()
        self.write("cmd/healthprobe/main_test.go", "package main\n")
        self.write("deploy/jaeger.yaml", "runtime: jaeger\n")
        self.write("deploy/otel-collector.yaml", "runtime: collector\n")
        self.assertEqual(before, self.all_digests())

    def test_invalid_or_missing_scope_fails_without_a_digest(self) -> None:
        for arguments in ([], ["missing"], ["../order"], ["--unknown"], ["order", "payment"]):
            with self.subTest(arguments=arguments):
                result = subprocess.run(
                    ["sh", str(self.root / "scripts/source-fingerprint.sh"), *arguments],
                    cwd=self.outside,
                    text=True,
                    capture_output=True,
                    timeout=15,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(result.stdout, "")
                self.assertTrue(result.stderr)

    def test_a_filename_containing_a_newline_fails_instead_of_being_ignored(self) -> None:
        self.write("services/order/line\nbreak.go", "package order\n")
        result = subprocess.run(
            ["sh", str(self.root / "scripts/source-fingerprint.sh"), "order"],
            cwd=self.outside,
            text=True,
            capture_output=True,
            timeout=15,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")

    def test_a_runtime_symlink_fails_instead_of_silently_missing_target_changes(self) -> None:
        (self.root / "web/dist/link.html").symlink_to("index.html")
        result = subprocess.run(
            ["sh", str(self.root / "scripts/source-fingerprint.sh"), "web"],
            cwd=self.outside,
            text=True,
            capture_output=True,
            timeout=15,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()
