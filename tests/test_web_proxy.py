"""Isolated real-Nginx HTTP checks, opt in with LUMO_TEST_WEB_IMAGE.

Uses an existing locally built frontend image, a temporary Docker network and
containers, and a tiny Go HTTP peer. It never changes the running Compose stack.
The actual image entrypoint renders the proxy template before nginx starts.
"""

from __future__ import annotations

import http.cookiejar
import json
import gzip
import os
import re
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import unittest
import urllib.error
import urllib.request
import uuid

PROJECT = Path(__file__).resolve().parents[1]
PEER = r'''package main
import ("encoding/json";"fmt";"io";"net/http";"os";"time")
func main(){
 http.HandleFunc("/",func(w http.ResponseWriter,r *http.Request){
  fmt.Println(r.URL.RequestURI())
  if r.URL.Path=="/api/v1/notification/stream" {
   w.Header().Set("Content-Type","text/event-stream")
   fmt.Fprint(w,"event: notifications\n")
   w.(http.Flusher).Flush()
   time.Sleep(2*time.Second)
   fmt.Fprintf(w,"data: %s|%s\n\n",r.Header.Get("Authorization"),r.Header.Get("Last-Event-ID"))
   return
  }
  w.Header().Set("Content-Type","application/json")
  w.Header().Set("X-Trace-Id","upstream-fixture")
  if r.URL.Path=="/api/v1/missing" {w.WriteHeader(404);fmt.Fprint(w,"api-missing");return}
  http.SetCookie(w,&http.Cookie{Name:"lumo_popup_session",Value:"fixture-session",Path:"/",HttpOnly:true,SameSite:http.SameSiteLaxMode})
  body,_:=io.ReadAll(r.Body)
  json.NewEncoder(w).Encode(map[string]string{"source":os.Getenv("PEER_SOURCE"),"uri":r.URL.RequestURI(),"method":r.Method,"body":string(body),"authorization":r.Header.Get("Authorization"),"cookie":r.Header.Get("Cookie"),"host":r.Host,"proto":r.Header.Get("X-Forwarded-Proto")})
 })
 if e:=http.ListenAndServe(":8080",nil);e!=nil{panic(e)}
}'''


@unittest.skipUnless(os.environ.get("LUMO_TEST_WEB_IMAGE"), "set LUMO_TEST_WEB_IMAGE to a locally built frontend image")
class WebProxyTests(unittest.TestCase):
    @classmethod
    def docker(cls, *args: str) -> str:
        result = subprocess.run(["docker", *args], capture_output=True, text=True, timeout=45)
        if result.returncode:
            raise RuntimeError("docker " + " ".join(args[:3]) + ": " + result.stderr)
        return result.stdout.strip()

    @classmethod
    def setUpClass(cls):
        cls.image = os.environ["LUMO_TEST_WEB_IMAGE"]
        cls.temporary = tempfile.TemporaryDirectory(prefix="lumo-web-proxy-")
        cls.addClassCleanup(cls.temporary.cleanup)
        cls.root = Path(cls.temporary.name)
        cls.network = "lumo-web-test-" + uuid.uuid4().hex[:10]
        cls.containers: list[str] = []
        cls.addClassCleanup(cls.cleanup_containers)
        architecture = cls.docker("image", "inspect", cls.image, "--format", "{{.Architecture}}")
        (cls.root / "peer.go").write_text(PEER)
        build = subprocess.run(
            ["go", "build", "-o", str(cls.root / "peer"), str(cls.root / "peer.go")],
            capture_output=True,
            text=True,
            timeout=60,
            env=dict(os.environ, GOOS="linux", GOARCH=architecture, CGO_ENABLED="0", GOWORK="off", GO111MODULE="off"),
        )
        if build.returncode:
            raise RuntimeError(build.stderr)
        cls.docker("network", "create", cls.network)
        cls.peer = cls.start_peer("gateway-one")
        static = cls.root / "static"
        static.mkdir()
        for name in ("index.html", "admin.html", "app.js", "styles.css"):
            shutil.copy2(PROJECT / "web" / "dist" / name, static / name)
        (static / "cache-fixture.deadbeef.js").write_text("const fixture = true;\n")
        cls.web = cls.docker(
            "run",
            "--rm",
            "-d",
            "--read-only",
            "--no-healthcheck",
            "--tmpfs",
            "/tmp:size=32m",
            "--tmpfs",
            "/etc/nginx/conf.d:size=1m",
            "--network",
            cls.network,
            "-p",
            "127.0.0.1::8080",
            "-e",
            "GATEWAY_UPSTREAM=gateway:8080",
            "-v",
            f"{static}:/usr/share/nginx/html:ro",
            cls.image,
        )
        cls.containers.append(cls.web)
        binding = cls.docker("port", cls.web, "8080").splitlines()[0]
        cls.base = "http://" + binding
        cls.await_source("gateway-one")

    @classmethod
    def start_peer(cls, source: str) -> str:
        peer = cls.docker(
            "run",
            "--rm",
            "-d",
            "--no-healthcheck",
            "--network",
            cls.network,
            "--network-alias",
            "gateway",
            "-e",
            "PEER_SOURCE=" + source,
            "--entrypoint",
            "/peer",
            "-v",
            f"{cls.root / 'peer'}:/peer:ro",
            cls.image,
        )
        cls.containers.append(peer)
        return peer

    @classmethod
    def cleanup_containers(cls):
        for container in reversed(cls.containers):
            subprocess.run(["docker", "rm", "-f", container], capture_output=True, timeout=15)
        subprocess.run(["docker", "network", "rm", cls.network], capture_output=True, timeout=15)

    @classmethod
    def request(cls, path: str, data: bytes | None = None, headers: dict | None = None, opener=None):
        req = urllib.request.Request(cls.base + path, data=data, headers=headers or {})
        try:
            response = opener.open(req, timeout=5) if opener else urllib.request.urlopen(req, timeout=5)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status, response.headers, response.read()

    @classmethod
    def await_source(cls, source: str):
        until = time.monotonic() + 25
        while time.monotonic() < until:
            try:
                status, _, body = cls.request("/readyz")
                if status == 200 and json.loads(body)["source"] == source:
                    return
            except (OSError, ValueError):
                pass
            time.sleep(0.2)
        raise AssertionError("web proxy did not resolve " + source + ": " + cls.docker("logs", cls.web))

    def test_api_keeps_method_payload_auth_cookie_and_upstream_errors(self):
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        payload = b'{"data":{"query":"phone"}}'
        status, headers, body = self.request(
            "/api/v1/catalog/list?limit=6",
            payload,
            {"Authorization": "Bearer fixture-token", "Content-Type": "application/json"},
            opener,
        )
        self.assertEqual(status, 200)
        data = json.loads(body)
        self.assertEqual(data["method"], "POST")
        self.assertEqual(data["uri"], "/api/v1/catalog/list?limit=6")
        self.assertEqual(data["body"], payload.decode())
        self.assertEqual(data["authorization"], "Bearer fixture-token")
        self.assertEqual(data["proto"], "http")
        self.assertEqual(data["host"], self.base.removeprefix("http://"))
        self.assertIn("HttpOnly", headers["Set-Cookie"])
        self.assertEqual(headers["Cache-Control"], "no-store")
        _, _, second = self.request("/api/v1/storefront/popup", b"{}", opener=opener)
        self.assertIn("lumo_popup_session=fixture-session", json.loads(second)["cookie"])
        status, _, body = self.request("/api/v1/missing")
        self.assertEqual((status, body), (404, b"api-missing"))

    def test_health_and_readiness_reach_gateway(self):
        for path in ("/healthz", "/readyz"):
            status, headers, body = self.request(path)
            self.assertEqual(status, 200)
            self.assertEqual(json.loads(body)["uri"], path)
            self.assertEqual(headers["Cache-Control"], "no-store")

    def test_notification_stream_flushes_and_preserves_session_headers(self):
        request = urllib.request.Request(
            self.base + "/api/v1/notification/stream",
            headers={
                "Authorization": "Bearer fixture-token",
                "Last-Event-ID": "previous-frame",
                "Accept-Encoding": "gzip",
            },
        )
        started = time.monotonic()
        with urllib.request.urlopen(request, timeout=5) as response:
            self.assertEqual(response.headers.get_content_type(), "text/event-stream")
            self.assertIsNone(response.headers.get("Content-Encoding"))
            self.assertEqual(response.readline(), b"event: notifications\n")
            self.assertLess(time.monotonic() - started, 1.5, "Nginx buffered the initial event")
            self.assertIn(b"Bearer fixture-token|previous-frame", response.read())

    def test_invalid_upstream_is_rejected_before_nginx_starts(self):
        for upstream in ("gateway:0", "gateway:99999", "gateway:8080;malformed"):
            result = subprocess.run(
                [
                    "docker",
                    "run",
                    "--rm",
                    "--read-only",
                    "--tmpfs",
                    "/tmp:size=32m",
                    "--tmpfs",
                    "/etc/nginx/conf.d:size=1m",
                    "-e",
                    "GATEWAY_UPSTREAM=" + upstream,
                    self.image,
                ],
                capture_output=True,
                text=True,
                timeout=15,
            )
            self.assertNotEqual(result.returncode, 0, upstream)
            self.assertIn("GATEWAY_UPSTREAM", result.stderr)

    def test_static_cache_revalidation_and_404_never_reach_gateway(self):
        before = self.docker("logs", self.peer)
        for path in ("/", "/admin.html", "/app.js", "/styles.css"):
            status, headers, body = self.request(path)
            self.assertEqual(status, 200, path)
            self.assertGreater(len(body), 0)
            self.assertEqual(headers["Cache-Control"], "no-cache")
            self.assertNotIn("X-Trace-Id", headers)
            self.assertEqual(headers["X-Content-Type-Options"], "nosniff")
        self.assertIn("max-age=3600", self.request("/app.js?v=release-1")[1]["Cache-Control"])
        self.assertNotIn("immutable", self.request("/app.js?v=release-1")[1]["Cache-Control"])
        self.assertEqual(self.request("/index.html?v=release-1")[1]["Cache-Control"], "no-cache")
        self.assertIn("immutable", self.request("/cache-fixture.deadbeef.js")[1]["Cache-Control"])
        for path in (
            "/missing.js",
            "/missing.deadbeef.js",
            "/missing.css",
            "/missing.png",
            "/unknown-route",
            "/.env",
            "/%2e%2e/etc/passwd",
        ):
            status, headers, _ = self.request(path)
            self.assertIn(status, (400, 404), path)
            self.assertNotIn("immutable", headers.get("Cache-Control", ""))
        self.assertEqual(before, self.docker("logs", self.peer))

    def test_built_assets_are_hashed_immutable_and_gzipped(self):
        production = self.docker(
            "run",
            "--rm",
            "-d",
            "--read-only",
            "--no-healthcheck",
            "--tmpfs",
            "/tmp:size=32m",
            "--tmpfs",
            "/etc/nginx/conf.d:size=1m",
            "--network",
            self.network,
            "-p",
            "127.0.0.1::8080",
            "-e",
            "GATEWAY_UPSTREAM=gateway:8080",
            self.image,
        )
        self.containers.append(production)
        base = "http://" + self.docker("port", production, "8080").splitlines()[0]
        deadline = time.monotonic() + 15
        while True:
            try:
                response = urllib.request.urlopen(base, timeout=3)
                break
            except (OSError, urllib.error.URLError):
                if time.monotonic() >= deadline:
                    raise
                time.sleep(0.2)
        with response:
            html = response.read().decode()
            self.assertEqual(response.headers["Cache-Control"], "no-cache")
        bundles = re.findall(r'(?:src|href)="([^"/]+\.[0-9a-f]{12}\.(?:js|css))"', html)
        self.assertEqual(len(bundles), 2)
        for bundle in bundles:
            request = urllib.request.Request(base + "/" + bundle, headers={"Accept-Encoding": "gzip"})
            with urllib.request.urlopen(request) as response:
                self.assertEqual(response.headers["Content-Encoding"], "gzip")
                self.assertIn("immutable", response.headers["Cache-Control"])
                self.assertIn("Accept-Encoding", response.headers["Vary"])
                compressed = response.read()
            with urllib.request.urlopen(base + "/" + bundle) as response:
                self.assertEqual(gzip.decompress(compressed), response.read())

    def test_z_gateway_recreation_resolves_a_new_container_address(self):
        replacement = self.start_peer("gateway-two")
        template = "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}"
        old_ip = self.docker("inspect", "--format", template, self.peer)
        new_ip = self.docker("inspect", "--format", template, replacement)
        self.assertNotEqual(old_ip, new_ip)
        self.docker("rm", "-f", self.peer)
        self.await_source("gateway-two")


if __name__ == "__main__":
    unittest.main()
