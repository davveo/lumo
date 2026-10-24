"""Incremental startup regression tests; no real Docker daemon or user data used.

Run: python3 tests/test_docker_start.py
The actual shell entrypoints and fingerprint helper run in a temporary checkout.
A persistent fake Docker CLI models image tags, container identity and health.
"""

from __future__ import annotations
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

PROJECT = Path(__file__).resolve().parents[1]
DOMAINS = (
    "iam member address catalog media interaction engagement search recommendation content analytics cart "
    "pricing checkout order campaign coupon points group flash inventory fulfillment logistics aftersale "
    "payment refund invoice reconciliation notification helpdesk"
).split()
BACKENDS = DOMAINS + ["gateway"]
APPS = BACKENDS + ["web"]
INFRA = ["consul", "mysql", "redis", "rabbitmq", "minio", "jaeger", "otel-collector"]
INIT = "storage-volume-init"
APP_IMAGE = "lumo-mall-backend:dev"
WEB_IMAGE = "lumo-mall-web:dev"
MINIO_IMAGE = "lumo-minio:release-20250907"
JAEGER_IMAGE = "lumo-jaeger:2.12.0"
COLLECTOR_IMAGE = "lumo-otel-collector:0.124.1"
BUILDABLE_INFRA = {"minio": MINIO_IMAGE, "jaeger": JAEGER_IMAGE, "otel-collector": COLLECTOR_IMAGE}

FAKE_DOCKER = r'''import hashlib,json,os,pathlib,re,sys
args=sys.argv[1:]
with open(os.environ['TEST_DOCKER_RECORD'],'a',encoding='utf-8') as stream:
    stream.write(json.dumps({'args':args,'cwd':os.getcwd()})+'\n')
path=pathlib.Path(os.environ['TEST_DOCKER_RUNTIME']);state=json.loads(path.read_text())
failure=os.environ.get('TEST_DOCKER_FAIL_STAGE','')
apps=state['domains']+['gateway','web'];infra=state['infra'];initializer='storage-volume-init';services=infra+[initializer]+apps
local_images={'minio':'lumo-minio:release-20250907','jaeger':'lumo-jaeger:2.12.0','otel-collector':'lumo-otel-collector:0.124.1'}
images={n:('lumo-mall-web:dev' if n=='web' else 'lumo-mall-backend:dev' if n in apps else local_images.get(n,'fixture-'+n+':fixed')) for n in services}
def save():path.write_text(json.dumps(state))
def die(message,code=1):print(message,file=sys.stderr);raise SystemExit(code)
def config_hash(name):return hashlib.sha256((name+'\0'+state.get('config_overrides',{}).get(name,'v1')).encode()).hexdigest()
def container(value):return next((c for n,c in state['containers'].items() if value in (n,c['id'],c.get('name',''))),None)
def instances(name):return [(key,c) for key,c in state['containers'].items() if c.get('service',key)==name]
def render(fmt,c):
    if 'json .' in fmt:
        st={'Status':c['status'],'Running':c['status']=='running','ExitCode':c['exit_code']}
        if c['health']!='none':st['Health']={'Status':c['health']}
        return json.dumps(st if 'json .State' in fmt else {'Id':c['id'],'Image':c['image'],'State':st,'Config':{'Labels':{'com.docker.compose.config-hash':c['config_hash']}}})
    fmt=re.sub(r'{{\s*if\s+\.State\.Health\s*}}.*?{{\s*else\s*}}(.*?){{\s*end\s*}}',lambda m:c['health'] if c['health']!='none' else m.group(1),fmt)
    fmt=re.sub(r'{{\s*if\s+\.State\.Health\s*}}.*?{{\s*end\s*}}',lambda m:c['health'] if c['health']!='none' else '',fmt)
    fmt=re.sub(r'{{\s*with\s+index\s+\.Config\.Labels\s+"com.docker.compose.config-hash"\s*}}.*?{{\s*else\s*}}.*?{{\s*end\s*}}',c['config_hash'],fmt)
    fmt=re.sub(r'{{\s*index\s+\.Config\.Labels\s+"com.docker.compose.config-hash"\s*}}',c['config_hash'],fmt)
    fields={'.State.Status':c['status'],'.State.Running':str(c['status']=='running').lower(),'.State.Health.Status':c['health'],'.State.ExitCode':str(c['exit_code']),'.Image':c['image'],'.Id':c['id'],'.ID':c['id'],'.Name':'/'+c.get('name',c['id'])}
    for key,value in fields.items():fmt=re.sub(r'{{\s*'+re.escape(key)+r'\s*}}',lambda _:value,fmt)
    if '{{' in fmt:die('Unsupported inspect template: '+fmt,95)
    return fmt
if args and args[0]=='info':
    if failure=='daemon':die('Cannot connect to Docker daemon',14)
    print(state.get('engine_id','daemon-fixture') if '--format' in args or '-f' in args else 'Fake Docker daemon');raise SystemExit(0)
if args[:2]==['context','show']:print('fixture-desktop');raise SystemExit(0)
if args[:2]==['image','inspect']:
    found=[v for v in args[2:] if v in state['images']]
    if not found:raise SystemExit(1)
    image_id=state['images'][found[0]]
    print(image_id if '--format' in args or '-f' in args else json.dumps([{'Id':image_id}]));raise SystemExit(0)
if args and args[0]=='inspect':
    found=[container(v) for v in args[1:] if container(v)]
    if not found:raise SystemExit(1)
    c=found[-1];flag='--format' if '--format' in args else '-f' if '-f' in args else None
    print(render(args[args.index(flag)+1],c) if flag else '['+render('{{json .}}',c)+']');raise SystemExit(0)
if not args or args[0]!='compose':die('Unexpected Docker command '+repr(args),90)
rest=args[1:];commands={'version','config','build','up','port','ps','logs','down','restart','start','wait','exec'}
idx=None;skip=False
for i,value in enumerate(rest):
    if skip:skip=False;continue
    if value in {'--project-directory','--env-file','--file','-f','--project-name','-p'}:skip=True;continue
    if value in commands:idx=i;break
if idx is None:die('Unexpected Compose command '+repr(rest),91)
command=rest[idx];tail=rest[idx+1:]
if command=='version':
    if failure=='compose':die('Compose plugin unavailable',12)
    print('2.35.1' if '--short' in tail else 'Docker Compose version v2.35.1')
elif command=='config':
    if '--help' in tail:print('--hash SERVICE --images --services --quiet --format');raise SystemExit(0)
    if failure=='config':die('invalid fixture Compose config',15)
    if '--services' in tail:print('\n'.join(services))
    elif '--hash' in tail:
        chosen=tail[tail.index('--hash')+1]
        print('\n'.join(n+' '+config_hash(n) for n in services if chosen=='*' or n==chosen))
    elif '--images' in tail:
        # Actual Compose ignores positional service filters for --images.
        print('\n'.join(dict.fromkeys(images.values())))
    elif '--format' in tail:print(json.dumps({'name':state.get('project_name','lumo-mall'),'services':{n:{'image':images[n]} for n in services}}))
    elif '--quiet' not in tail and '-q' not in tail:
        print('name: '+state.get('project_name','lumo-mall')+'\nservices:')
        for n in services:
            print('  '+n+':\n    image: '+images[n])
            if n=='web':print('    build:\n      args:\n        NGINX_IMAGE: '+state.get('web_base_image','nginx:1.30.6-alpine'))
elif command=='up' and '--help' in tail:
    print('Usage: docker compose up [OPTIONS] [SERVICE...]\n--no-deps --no-recreate --force-recreate --wait')
    if failure!='wait_support':print('--wait-timeout int')
elif command=='build':
    if failure=='build':die('fixture image build failed',19)
    targets=[n for n in tail if n in services] or services
    for image in dict.fromkeys(images[n] for n in targets):
        state['sequence']+=1;state['images'][image]='sha256:built-'+str(state['sequence'])
    mutation=os.environ.get('TEST_DOCKER_MUTATE_ON_BUILD')
    if mutation:
        p=pathlib.Path(os.getcwd())/mutation;p.write_text(p.read_text()+'\n// edited during build\n')
    save()
elif command=='ps':
    chosen=[n for n in tail if n in services] or services
    if '--quiet' in tail or '-q' in tail:
        all_states='--all' in tail or '-a' in tail
        print('\n'.join(c['id'] for n in chosen for _,c in instances(n) if all_states or c['status']=='running'))
    elif '--format' in tail:print(json.dumps([{'Service':n,'ID':c['id'],'State':c['status'],'Health':c['health']} for n in chosen for _,c in instances(n)]))
elif command=='up':
    selected=[];skip=False;scales={}
    for i,value in enumerate(tail):
        if value=='--scale':
            name,count=tail[i+1].split('=',1);scales[name]=int(count)
    for value in tail:
        if skip:skip=False;continue
        if value in {'--wait-timeout','--exit-code-from','--timeout','-t','--pull','--scale'}:skip=True;continue
        if value in services:selected.append(value)
    selected=selected or services[:];is_app=any(n in apps for n in selected)
    if failure=='up' or failure=='app_up' and is_app or failure=='infra_up' and not is_app:die('fixture health wait failure',23)
    for n in selected:
        current=instances(n);count=scales.get(n,len(current) or 1)
        for key,_ in current[count:]:del state['containers'][key]
        for index in range(count):
            key=n if index==0 else n+'#'+str(index+1)
            old=state['containers'].get(key);image=state['images'].get(images[n],old['image'] if old else 'sha256:fixture-'+n)
            replace=not old or '--force-recreate' in tail or ('--no-recreate' not in tail and (old['image']!=image or old['config_hash']!=config_hash(n)))
            if replace:
                state['images'].setdefault(images[n],image)
                state['sequence']+=1;old={'id':'cid-'+n+'-'+str(state['sequence']),'name':'lumo-'+key,'service':n,'image':image,'config_hash':config_hash(n)};state['containers'][key]=old
            old.update(status='exited' if n==initializer else 'running',health='none' if n==initializer else 'healthy',exit_code=0)
    save()
elif command=='port':
    ports={('web','8080'):'19080',('gateway','8080'):'29080',('consul','8500'):'18500',('jaeger','16686'):'26686',('rabbitmq','15672'):'25672',('minio','9000'):'19001',('minio','9001'):'19002'}
    key=tuple([v for v in tail if not v.startswith('-')][-2:])
    if key not in ports:die('Unexpected port lookup '+repr(tail),92)
    if key[0] not in state['containers']:raise SystemExit(1)
    binding=state.get('port_bindings',{}).get(':'.join(key),'127.0.0.1:'+ports[key])
    if binding:print(binding)
elif command=='exec':
    if len(tail)!=4 or tail[0]!='-T' or tail[2]!='printenv':die('Unexpected container command '+repr(tail),94)
    value=state.get('service_env',{}).get(tail[1],{}).get(tail[3])
    if value is None:raise SystemExit(1)
    print(value)
elif command in ('down','restart','start'):die('Unexpected broad lifecycle operation '+command,93)
elif command=='wait':
    for n in tail:
        if n in state['containers']:print(state['containers'][n]['exit_code'])
'''


class DockerStartupTests(unittest.TestCase):
    maxDiff = None

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="lumo incremental startup ")
        self.addCleanup(self.temp.cleanup)
        self.sandbox = Path(self.temp.name)
        self.project = self.sandbox / "project with spaces"
        self.project.mkdir()
        for rel in (
            "start.sh",
            "start.command",
            "scripts/up.sh",
            "scripts/start-incremental.sh",
            "scripts/source-fingerprint.sh",
            "Dockerfile",
            ".dockerignore",
            "web/Dockerfile",
            "web/entrypoint.sh",
            "web/.dockerignore",
            "web/nginx/default.conf.template",
            "web/nginx/nginx.conf",
            "web/nginx/proxy.conf",
            "compose.yaml",
            "deploy/minio.Dockerfile",
        ):
            src = PROJECT / rel
            if not src.is_file():
                self.fail("Required startup source missing: " + rel)
            dest = self.project / rel
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(src, dest)
        files = {
            "go.mod": "module github.com/davveo/lumo\n\ngo 1.26\n",
            "go.sum": "fixture v1\n",
            "api/mall/v1/domain.proto": 'syntax = "proto3";\n',
            "internal/platform/platform.go": "package platform\nconst Version = 1\n",
            "internal/bootstrap/registry.go": "package bootstrap\n",
            "internal/democatalog/catalog.go": "package democatalog\n",
            "internal/httpgateway/gateway.go": "package httpgateway\n",
            "internal/devstack/stack.go": "package devstack\n",
            "cmd/server/main.go": "package main\nfunc main() {}\n",
            "cmd/gateway/main.go": "package main\nfunc main() {}\n",
            "cmd/devstack/main.go": "package main\nfunc main() {}\n",
            "web/dist/index.html": "<title>Lumo test</title>",
            "web/dist/admin.html": "<title>Lumo admin</title>",
            "web/dist/app.js": "const app = 'v1';\n",
            "deploy/jaeger.yaml": "fixture: jaeger\n",
            "deploy/otel-collector.yaml": "fixture: collector\n",
            "README.md": "Fixture docs\n",
            ".env.example": "WEB_PORT=19080\nGATEWAY_PORT=29080\nCUSTOM_KEEP=template\n",
        }
        for n in DOMAINS:
            files[f"services/{n}/service.go"] = f"package {n}\n"
            files[f"services/{n}/internal/usecase/service.go"] = "package usecase\nconst Policy = 1\n"
        for rel, text in files.items():
            p = self.project / rel
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text(text, encoding="utf-8")
        self.template = files[".env.example"]
        self.tools = self.sandbox / "fake tools"
        self.tools.mkdir()
        docker = self.tools / "docker"
        docker.write_text("#!" + sys.executable + "\n" + FAKE_DOCKER)
        docker.chmod(0o755)
        self.record = self.sandbox / "docker calls.jsonl"
        self.runtime = self.sandbox / "fake daemon.json"
        self.side_effect = self.sandbox / "env must not execute"
        images = {
            APP_IMAGE: "sha256:original-app",
            WEB_IMAGE: "sha256:original-web",
            **{tag: "sha256:original-" + name for name, tag in BUILDABLE_INFRA.items()},
        }
        containers = {}
        for n in INFRA + [INIT] + APPS:
            image = (
                images[WEB_IMAGE] if n == "web" else images[APP_IMAGE]
                if n in APPS
                else images[BUILDABLE_INFRA[n]] if n in BUILDABLE_INFRA else "sha256:fixture-" + n
            )
            containers[n] = {
                "id": "cid-" + n + "-original",
                "name": "lumo-" + n,
                "status": "exited" if n == INIT else "running",
                "health": "none" if n == INIT else "healthy",
                "exit_code": 0,
                "image": image,
                "config_hash": hashlib.sha256((n + "\0v1").encode()).hexdigest(),
            }
        self.write_runtime(
            {
                "domains": DOMAINS,
                "infra": INFRA,
                "engine_id": "daemon-fixture",
                "sequence": 0,
                "images": images,
                "containers": containers,
                "config_overrides": {},
                "service_env": {
                    "iam": {"DEMO_MODE": "true"},
                    "rabbitmq": {"RABBITMQ_DEFAULT_USER": "fixture-mq", "RABBITMQ_DEFAULT_PASS": "fixture-mq-password"},
                    "minio": {"MINIO_ROOT_USER": "fixture-minio", "MINIO_ROOT_PASSWORD": "fixture-minio-password"},
                },
            }
        )
        self.outside = self.sandbox / "unrelated working directory"
        self.outside.mkdir()
        self.env = {
            k: v
            for k, v in os.environ.items()
            if k
            not in {
                "COMPOSE_FILE",
                "COMPOSE_PROJECT_NAME",
                "COMPOSE_ENV_FILES",
                "DOCKER_HOST",
                "DOCKER_CONTEXT",
                "GATEWAY_PORT",
                "WEB_PORT",
                "WEB_GATEWAY_UPSTREAM",
                "JAEGER_PORT",
                "RABBITMQ_UI_PORT",
                "CONSUL_UI_PORT",
                "MINIO_API_PORT",
                "MINIO_CONSOLE_PORT",
                "WAIT_TIMEOUT",
                "STARTUP_TIMEOUT",
            }
        }
        self.env.update(
            PATH=str(self.tools) + os.pathsep + os.environ.get("PATH", ""),
            TEST_DOCKER_RECORD=str(self.record),
            TEST_DOCKER_RUNTIME=str(self.runtime),
            SIDE_EFFECT_FILE=str(self.side_effect),
            TERM="dumb",
        )

    def read_runtime(self):
        return json.loads(self.runtime.read_text())

    def write_runtime(self, value):
        self.runtime.write_text(json.dumps(value))

    def mutate(self, rel, text="\n// changed runtime behavior\n"):
        p = self.project / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        with p.open("a") as stream:
            stream.write(text)

    def run_start(self, *args, entry="start.sh", fail="", extra_env=None):
        env = dict(self.env, TEST_DOCKER_FAIL_STAGE=fail)
        env.update(extra_env or {})
        return subprocess.run(
            ["sh", str(self.project / entry), *args],
            cwd=self.outside,
            env=env,
            stdin=subprocess.DEVNULL,
            text=True,
            capture_output=True,
            timeout=90,
        )

    def calls(self):
        return [json.loads(line) for line in self.record.read_text().splitlines()] if self.record.exists() else []

    def clear_calls(self):
        self.record.write_text("")

    def deployment_records(self):
        return {
            str(p.relative_to(self.project)): p.read_text()
            for p in (self.project / ".local" / "start").glob("*/*.state")
            if p.stem in APPS
        }

    @staticmethod
    def command(call):
        args = call["args"]
        if args[0] != "compose":
            return " ".join(args[:2]) if args[0] == "image" else args[0]
        return next(
            (
                v
                for v in args[1:]
                if v
                in {
                    "version",
                    "config",
                    "build",
                    "up",
                    "port",
                    "ps",
                    "logs",
                    "down",
                    "restart",
                    "start",
                    "wait",
                    "exec",
                }
            ),
            "",
        )

    @staticmethod
    def selected(call):
        command = DockerStartupTests.command(call)
        if command not in ("up", "build"):
            return set()
        result = set()
        skip = False
        for value in call["args"][call["args"].index(command) + 1 :]:
            if skip:
                skip = False
                continue
            if value in {"--wait-timeout", "--exit-code-from", "--timeout", "-t", "--pull", "--scale"}:
                skip = True
                continue
            if value in APPS + INFRA + [INIT]:
                result.add(value)
        return result

    def actual_ups(self):
        return [c for c in self.calls() if self.command(c) == "up" and "--help" not in c["args"]]

    def builds(self):
        return [c for c in self.calls() if self.command(c) == "build"]

    def app_targets(self):
        return set().union(*(self.selected(c) & set(APPS) for c in self.actual_ups())) if self.actual_ups() else set()

    def assert_success(self, result):
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse(any(self.command(c) in {"down", "restart", "start"} for c in self.calls()), self.calls())

    def establish_baseline(self):
        self.assert_success(self.run_start())
        self.clear_calls()

    def assert_infra_preserved(self, before):
        after = self.read_runtime()["containers"]
        for n in INFRA + [INIT]:
            self.assertEqual(after[n], before["containers"][n], n)

    def test_first_run_updates_apps_once_and_preserves_running_infrastructure(self):
        before = self.read_runtime()
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), set(APPS))
        self.assertEqual([self.selected(c) for c in self.builds()], [{"gateway"}, {"web"}])
        self.assertNotIn("minio", self.selected(self.builds()[0]))
        self.assert_infra_preserved(before)
        for call in self.actual_ups():
            self.assertIn("--no-deps", call["args"])
            self.assertIn("--no-build", call["args"])
            self.assertTrue(self.selected(call))

    def test_repeated_run_without_changes_does_not_build_or_restart_anything(self):
        self.establish_baseline()
        before = self.read_runtime()
        self.assert_success(self.run_start())
        self.assertEqual(self.builds(), [])
        self.assertEqual(self.actual_ups(), [])
        self.assertEqual(self.read_runtime(), before)

    def test_order_and_frontend_updates_do_not_dirty_other_containers_via_shared_tag(self):
        self.establish_baseline()
        before = self.read_runtime()
        self.mutate("services/order/internal/usecase/service.go")
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), {"order"})
        self.assertEqual(len(self.builds()), 1)
        self.assert_infra_preserved(before)
        state = self.read_runtime()
        self.assertNotEqual(state["containers"]["order"]["image"], state["containers"]["payment"]["image"])
        self.clear_calls()
        self.assert_success(self.run_start())
        self.assertEqual(self.actual_ups(), [])
        self.assertEqual(self.builds(), [])
        self.mutate("web/dist/app.js")
        self.clear_calls()
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), {"web"})
        self.assertEqual([self.selected(c) for c in self.builds()], [{"web"}])
        self.assertEqual(self.read_runtime()["containers"]["gateway"], state["containers"]["gateway"])
        self.clear_calls()
        self.assert_success(self.run_start())
        self.assertEqual(self.actual_ups(), [])

    def test_shared_platform_updates_all_apps_and_preserves_infrastructure(self):
        self.establish_baseline()
        before = self.read_runtime()
        self.mutate("internal/platform/platform.go")
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), set(BACKENDS))
        self.assertEqual(len(self.builds()), 1)
        self.assert_infra_preserved(before)

    def test_frontend_recipe_changes_build_only_web(self):
        self.establish_baseline()
        before = self.read_runtime()
        self.mutate("web/nginx/default.conf.template", "\n# changed proxy configuration\n")
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), {"web"})
        self.assertEqual([self.selected(c) for c in self.builds()], [{"web"}])
        after = self.read_runtime()
        self.assertEqual(after["images"][APP_IMAGE], before["images"][APP_IMAGE])
        for service in BACKENDS:
            self.assertEqual(after["containers"][service], before["containers"][service], service)
        self.assert_infra_preserved(before)

    def test_web_no_build_keeps_changed_frontend_pending(self):
        self.establish_baseline()
        before = self.read_runtime()
        self.mutate("web/dist/app.js")
        self.assert_success(self.run_start("--no-build"))
        self.assertEqual(self.app_targets(), {"web"})
        self.assertEqual(self.builds(), [])
        self.clear_calls()
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), {"web"})
        self.assertEqual([self.selected(c) for c in self.builds()], [{"web"}])
        self.assertEqual(self.read_runtime()["images"][APP_IMAGE], before["images"][APP_IMAGE])

    def test_changing_the_frontend_base_image_rebuilds_only_web(self):
        self.establish_baseline()
        state = self.read_runtime()
        original_backend = state["images"][APP_IMAGE]
        state["web_base_image"] = "local-nginx:1.26.3"
        self.write_runtime(state)
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), {"web"})
        self.assertEqual([self.selected(c) for c in self.builds()], [{"web"}])
        self.assertEqual(self.read_runtime()["images"][APP_IMAGE], original_backend)

    def test_legacy_gateway_port_moves_to_web_with_an_exact_private_backup(self):
        legacy = (
            "GATEWAY_PORT='19080' # original storefront\nCUSTOM_KEEP=literal $(do-not-execute)\n"
            "MINIO_CORS_ORIGINS=http://localhost:${GATEWAY_PORT:-8080},http://127.0.0.1:$GATEWAY_PORT\n"
        )
        (self.project / ".env").write_text(legacy)
        self.assert_success(self.run_start("order"))
        migrated = (self.project / ".env").read_text()
        self.assertIn("WEB_PORT=19080\n", migrated)
        self.assertIn("GATEWAY_PORT=18080\n", migrated)
        self.assertIn("CUSTOM_KEEP=literal $(do-not-execute)", migrated)
        self.assertIn("MINIO_CORS_ORIGINS=http://localhost:${WEB_PORT:-8080},http://127.0.0.1:$WEB_PORT", migrated)
        self.assertLess(migrated.index("WEB_PORT="), migrated.index("MINIO_CORS_ORIGINS="))
        backups = list((self.project / ".local" / "env-backups").glob("before-web-split.*"))
        self.assertEqual(len(backups), 1)
        self.assertEqual(backups[0].read_text(), legacy)
        self.assertEqual(backups[0].stat().st_mode & 0o777, 0o600)
        self.assert_success(self.run_start("order"))
        self.assertEqual(len(list(backups[0].parent.iterdir())), 1)

    def test_demo_catalog_updates_only_catalog_and_inventory(self):
        self.establish_baseline()
        self.mutate("internal/democatalog/catalog.go")
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), {"catalog", "inventory"})

    def test_documentation_tests_and_devstack_do_not_restart_runtime(self):
        self.establish_baseline()
        for p in (
            "README.md",
            "services/order/internal/usecase/service_test.go",
            "cmd/devstack/main.go",
            "internal/devstack/stack.go",
        ):
            self.mutate(p)
        self.assert_success(self.run_start())
        self.assertEqual(self.builds(), [])
        self.assertEqual(self.actual_ups(), [])

    def test_changed_resolved_order_config_only_updates_order_without_source_build(self):
        self.establish_baseline()
        state = self.read_runtime()
        state["config_overrides"]["order"] = "new-environment"
        self.write_runtime(state)
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), {"order"})
        self.assertEqual(self.builds(), [])

    def test_running_middleware_config_change_is_not_applied_implicitly(self):
        self.establish_baseline()
        state = self.read_runtime()
        before = json.loads(json.dumps(state))
        state["config_overrides"]["mysql"] = "changed"
        self.write_runtime(state)
        self.assert_success(self.run_start())
        self.assertEqual(self.actual_ups(), [])
        self.assertEqual(self.read_runtime()["containers"]["mysql"], before["containers"]["mysql"])

    def test_explicit_infrastructure_update_applies_configuration(self):
        self.establish_baseline()
        before = self.read_runtime()
        state = json.loads(json.dumps(before))
        state["config_overrides"]["mysql"] = "changed"
        self.write_runtime(state)
        self.assert_success(self.run_start("--update-infra"))
        self.assertEqual(self.app_targets(), set())
        self.assertEqual(set().union(*(self.selected(c) for c in self.actual_ups())), set(INFRA + [INIT]))
        for n in INFRA + [INIT]:
            self.assertNotEqual(self.read_runtime()["containers"][n]["id"], before["containers"][n]["id"], n)
        for n in APPS:
            self.assertEqual(self.read_runtime()["containers"][n], before["containers"][n], n)
        self.assertEqual([self.selected(c) for c in self.builds()], [set(BUILDABLE_INFRA)])

    def test_running_minio_does_not_require_its_old_local_image_tag(self):
        state = self.read_runtime()
        del state["images"][MINIO_IMAGE]
        self.write_runtime(state)
        before = self.read_runtime()
        self.assert_success(self.run_start("order"))
        self.assert_infra_preserved(before)
        self.assertEqual([self.selected(c) for c in self.builds()], [{"gateway"}])

    def test_stopped_minio_restores_missing_image_tag_without_recreating_container_or_initializer(self):
        state = self.read_runtime()
        state["containers"]["minio"]["status"] = "exited"
        del state["images"][MINIO_IMAGE]
        cid = state["containers"]["minio"]["id"]
        initializer = state["containers"][INIT]
        self.write_runtime(state)
        self.assert_success(self.run_start("order"))
        self.assertEqual(self.read_runtime()["containers"]["minio"]["id"], cid)
        self.assertEqual(self.read_runtime()["containers"][INIT], initializer)
        self.assertFalse(any(INIT in self.selected(c) for c in self.actual_ups()))
        # Compose resolves service images before it resumes stopped containers.
        self.assertEqual([self.selected(c) for c in self.builds()], [{"gateway"}, {"minio"}])

    def test_stopped_mysql_starts_same_container_without_rebuild(self):
        self.establish_baseline()
        state = self.read_runtime()
        state["containers"]["mysql"]["status"] = "exited"
        cid = state["containers"]["mysql"]["id"]
        self.write_runtime(state)
        self.assert_success(self.run_start())
        self.assertEqual(set().union(*(self.selected(c) for c in self.actual_ups())), {"mysql"})
        self.assertEqual(self.read_runtime()["containers"]["mysql"]["id"], cid)
        self.assertEqual(self.builds(), [])
        for call in self.actual_ups():
            self.assertIn("--no-recreate", call["args"])

    def test_missing_or_stopped_observability_builds_only_its_missing_local_image(self):
        self.establish_baseline()
        baseline = self.read_runtime()
        for service in ("jaeger", "otel-collector"):
            for status in ("missing", "exited"):
                with self.subTest(service=service, status=status):
                    state = json.loads(json.dumps(baseline))
                    original = state["containers"][service]
                    if status == "missing":
                        del state["containers"][service]
                    else:
                        state["containers"][service]["status"] = "exited"
                    del state["images"][BUILDABLE_INFRA[service]]
                    self.write_runtime(state)
                    self.clear_calls()
                    self.assert_success(self.run_start())
                    self.assertEqual([self.selected(c) for c in self.builds()], [{service}])
                    self.assertEqual(set().union(*(self.selected(c) for c in self.actual_ups())), {service})
                    after = self.read_runtime()["containers"]
                    self.assertEqual(after[service]["health"], "healthy")
                    if status == "exited":
                        self.assertEqual(after[service]["id"], original["id"])
                    for name in set(after) - {service}:
                        self.assertEqual(after[name], baseline["containers"][name], name)

    def test_running_observability_does_not_require_its_local_image_tags(self):
        self.establish_baseline()
        state = self.read_runtime()
        for service in ("jaeger", "otel-collector"):
            del state["images"][BUILDABLE_INFRA[service]]
        self.write_runtime(state)
        before = self.read_runtime()
        self.assert_success(self.run_start())
        self.assertEqual(self.builds(), [])
        self.assertEqual(self.actual_ups(), [])
        self.assertEqual(self.read_runtime(), before)

    def test_running_observability_without_healthy_checks_fails_before_building(self):
        baseline = self.read_runtime()
        for service in ("jaeger", "otel-collector"):
            for health in ("none", "unhealthy", "starting"):
                with self.subTest(service=service, health=health):
                    state = json.loads(json.dumps(baseline))
                    state["containers"][service]["health"] = health
                    self.write_runtime(state)
                    self.clear_calls()
                    result = self.run_start("order")
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn(service, result.stderr)
                    self.assertEqual(self.builds(), [])
                    self.assertEqual(self.actual_ups(), [])
                    self.assertEqual(self.read_runtime(), state)

    def test_no_build_requires_missing_observability_image_to_be_built_explicitly(self):
        self.establish_baseline()
        state = self.read_runtime()
        state["containers"]["jaeger"]["status"] = "exited"
        del state["images"][JAEGER_IMAGE]
        self.write_runtime(state)
        result = self.run_start("--no-build")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("--no-build", result.stderr)
        self.assertIn("jaeger", result.stderr)
        self.assertIn("缺少本地中间件镜像", result.stderr)
        self.assertEqual(self.builds(), [])
        self.assertEqual(self.actual_ups(), [])

    def test_explicit_infrastructure_no_build_reuses_existing_local_images(self):
        self.establish_baseline()
        before = self.read_runtime()
        self.assert_success(self.run_start("--update-infra", "--no-build"))
        self.assertEqual(self.builds(), [])
        after = self.read_runtime()["containers"]
        for name in INFRA + [INIT]:
            self.assertNotEqual(after[name]["id"], before["containers"][name]["id"], name)
        for name in APPS:
            self.assertEqual(after[name], before["containers"][name], name)

    def test_missing_or_stopped_consul_is_started_without_rebuilding_applications(self):
        self.establish_baseline()
        for status in ("missing", "exited"):
            with self.subTest(status=status):
                state = self.read_runtime()
                before = json.loads(json.dumps(state["containers"]))
                if status == "missing":
                    del state["containers"]["consul"]
                else:
                    state["containers"]["consul"]["status"] = "exited"
                self.write_runtime(state)
                self.clear_calls()
                result = self.run_start()
                self.assert_success(result)
                self.assertEqual(set().union(*(self.selected(c) for c in self.actual_ups())), {"consul"})
                self.assertEqual(self.builds(), [])
                self.assertIn("Consul注册中心：http://127.0.0.1:18500/ui/", result.stdout)
                after = self.read_runtime()["containers"]
                self.assertEqual(after["consul"]["status"], "running")
                if status == "exited":
                    self.assertEqual(after["consul"]["id"], before["consul"]["id"])
                for name in set(before) - {"consul"}:
                    self.assertEqual(after[name], before[name], name)
                for call in self.actual_ups():
                    self.assertIn("--no-recreate", call["args"])

    def test_stopped_unchanged_app_starts_same_container_without_rebuild(self):
        self.establish_baseline()
        state = self.read_runtime()
        state["containers"]["order"]["status"] = "exited"
        cid = state["containers"]["order"]["id"]
        self.write_runtime(state)
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), {"order"})
        self.assertEqual(self.read_runtime()["containers"]["order"]["id"], cid)
        self.assertEqual(self.builds(), [])
        for call in self.actual_ups():
            self.assertIn("--no-recreate", call["args"])

    def add_replica(self, service):
        state = self.read_runtime()
        replica = json.loads(json.dumps(state['containers'][service]))
        replica.update(id='cid-' + service + '-second', name='lumo-' + service + '-second', service=service)
        state['containers'][service + '#2'] = replica
        self.write_runtime(state)

    def test_multiple_healthy_replicas_are_retained_without_restarts(self):
        self.establish_baseline()
        self.add_replica('catalog')
        before = self.read_runtime()
        self.assert_success(self.run_start())
        self.assertEqual(self.actual_ups(), [])
        self.assertEqual(self.builds(), [])
        self.assertEqual(self.read_runtime(), before)

    def test_incremental_update_preserves_every_replica_and_checks_all_images(self):
        self.establish_baseline()
        self.add_replica('catalog')
        before = self.read_runtime()
        self.mutate('services/catalog/internal/usecase/service.go')
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), {'catalog'})
        after = self.read_runtime()['containers']
        for key in ('catalog', 'catalog#2'):
            self.assertNotEqual(after[key]['id'], before['containers'][key]['id'])
            self.assertEqual(after[key]['image'], self.read_runtime()['images'][APP_IMAGE])
        self.assertEqual(len([c for key, c in after.items() if c.get('service', key) == 'catalog']), 2)
        self.assertTrue(any('--scale' in c['args'] and 'catalog=2' in c['args'] for c in self.actual_ups()))
        self.assert_infra_preserved(before)
        self.clear_calls()
        self.assert_success(self.run_start())
        self.assertEqual(self.actual_ups(), [])

    def test_stopped_second_replica_is_started_without_collapsing_the_service(self):
        self.establish_baseline()
        self.add_replica('catalog')
        state = self.read_runtime()
        state['containers']['catalog#2']['status'] = 'exited'
        self.write_runtime(state)
        self.assert_success(self.run_start())
        after = self.read_runtime()['containers']
        self.assertEqual(self.app_targets(), {'catalog'})
        self.assertEqual(self.builds(), [])
        for key in ('catalog', 'catalog#2'):
            self.assertEqual(after[key]['id'], state['containers'][key]['id'])
            self.assertEqual(after[key]['status'], 'running')
        self.assertTrue(any('--no-recreate' in c['args'] and 'catalog=2' in c['args'] for c in self.actual_ups()))

    def test_stale_second_replica_forces_consistent_service_update(self):
        self.establish_baseline()
        self.add_replica('catalog')
        state = self.read_runtime()
        state['containers']['catalog#2']['image'] = 'sha256:stale-replica'
        self.write_runtime(state)
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), {'catalog'})
        after = self.read_runtime()['containers']
        self.assertEqual(after['catalog']['image'], after['catalog#2']['image'])
        self.assertTrue(any('catalog=2' in c['args'] for c in self.actual_ups()))

    def test_first_explicit_service_does_not_update_other_apps(self):
        before = self.read_runtime()
        self.assert_success(self.run_start("order"))
        self.assertEqual(self.app_targets(), {"order"})
        for n in set(APPS) - {"order"}:
            self.assertEqual(self.read_runtime()["containers"][n], before["containers"][n], n)
        self.assert_infra_preserved(before)

    def test_baselines_are_isolated_by_docker_engine_and_resolved_project_name(self):
        self.assert_success(self.run_start("order"))
        self.clear_calls()
        state = self.read_runtime()
        state["engine_id"] = "another-daemon"
        self.write_runtime(state)
        self.assert_success(self.run_start("order"))
        self.assertEqual(len(self.builds()), 1)
        self.clear_calls()
        state = self.read_runtime()
        state["project_name"] = "another-project"
        self.write_runtime(state)
        self.assert_success(self.run_start("order"))
        self.assertEqual(len(self.builds()), 1)
        baselines = list((self.project / ".local" / "start").glob("*/order.state"))
        self.assertEqual(len(baselines), 3)
        for path in baselines:
            self.assertEqual(len(path.read_text().splitlines()), 3)

    def test_no_build_leaves_dirty_source_pending_for_next_normal_start(self):
        self.establish_baseline()
        self.mutate("services/order/internal/usecase/service.go")
        self.assert_success(self.run_start("--no-build"))
        self.assertEqual(self.builds(), [])
        self.clear_calls()
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), {"order"})
        self.assertEqual(len(self.builds()), 1)

    def test_build_failure_does_not_advance_deployment_baseline(self):
        self.establish_baseline()
        self.mutate("services/order/internal/usecase/service.go")
        before = self.deployment_records()
        result = self.run_start(fail="build")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.actual_ups(), [])
        self.assertNotIn("启动完成", result.stdout)
        self.assertEqual(self.deployment_records(), before)
        self.clear_calls()
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), {"order"})
        self.assertEqual(len(self.builds()), 1)

    def test_health_failure_does_not_mark_changed_code_deployed(self):
        self.establish_baseline()
        self.mutate("services/order/internal/usecase/service.go")
        before = self.deployment_records()
        result = self.run_start(fail="app_up")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("启动完成", result.stdout)
        self.assertEqual(self.deployment_records(), before)
        self.clear_calls()
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), {"order"})

    def test_edit_during_build_is_not_misrecorded_as_deployed(self):
        self.establish_baseline()
        self.mutate("services/order/internal/usecase/service.go")
        before = self.deployment_records()
        result = self.run_start(extra_env={"TEST_DOCKER_MUTATE_ON_BUILD": "services/order/internal/usecase/service.go"})
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.app_targets(), set())
        self.assertEqual(self.deployment_records(), before)
        self.clear_calls()
        self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(), {"order"})

    def test_fresh_install_runs_initializer_once_and_starts_dependencies(self):
        state = self.read_runtime()
        state["containers"] = {}
        state["images"] = {}
        self.write_runtime(state)
        self.assert_success(self.run_start())
        self.assertEqual(set().union(*(self.selected(c) for c in self.actual_ups())), set(APPS + INFRA + [INIT]))
        self.assertEqual(sum(INIT in self.selected(c) for c in self.actual_ups()), 1)
        self.clear_calls()
        self.assert_success(self.run_start())
        self.assertEqual(self.actual_ups(), [])

    def test_dynamic_ports_and_legacy_mac_wrappers_still_work(self):
        for entry in ("start.sh", "scripts/up.sh", "start.command"):
            with self.subTest(entry=entry):
                self.clear_calls()
                result = self.run_start("order", "--wait-timeout", "71", entry=entry)
                self.assert_success(result)
                for port in ("19080", "29080", "18500", "26686", "25672", "19001", "19002"):
                    self.assertIn(":" + port, result.stdout)
                self.assertIn("/admin.html", result.stdout)
                self.assertNotIn("http://localhost:8080", result.stdout)
                self.assertIn("可访问地址（在Docker宿主机浏览器打开）：", result.stdout)
                for line in result.stdout.splitlines():
                    if "http://" in line:
                        self.assertTrue(line.startswith("  "), line)
                for call in self.actual_ups():
                    if "--wait-timeout" in call["args"]:
                        self.assertEqual(call["args"][call["args"].index("--wait-timeout") + 1], "71")

    def test_accessible_addresses_normalize_wildcards_use_first_binding_and_skip_empty(self):
        state = self.read_runtime()
        state["port_bindings"] = {
            "web:8080": "0.0.0.0:19080\n[::]:19080",
            "gateway:8080": "0.0.0.0:29080\n[::]:29080",
            "jaeger:16686": "[::]:26686\n0.0.0.0:26686",
            "rabbitmq:15672": "192.0.2.27:25672",
            "minio:9000": "",
        }
        self.write_runtime(state)
        result = self.run_start("order")
        self.assert_success(result)
        self.assertIn("可访问地址（在Docker宿主机浏览器打开）：", result.stdout)
        for expected in (
            "商城前台：http://127.0.0.1:19080",
            "商城管理端：http://127.0.0.1:19080/admin.html",
            "API网关：http://127.0.0.1:29080/api/v1",
            "接口联调台：http://127.0.0.1:29080/debug",
            "Consul注册中心：http://127.0.0.1:18500/ui/",
            "Jaeger：http://[::1]:26686",
            "RabbitMQ：http://192.0.2.27:25672",
            "MinIO控制台：http://127.0.0.1:19002",
        ):
            self.assertIn("  " + expected, result.stdout)
        self.assertNotIn("0.0.0.0", result.stdout)
        self.assertNotIn("[::]:", result.stdout)
        self.assertNotIn("MinIO接口：", result.stdout)
        links = [line for line in result.stdout.splitlines() if "http://" in line]
        self.assertEqual(len(links), 8)
        for line in links:
            self.assertTrue(line.startswith("  "), line)

    def test_startup_lists_matching_login_details_below_access_addresses(self):
        result = self.run_start("order")
        self.assert_success(result)
        for expected in (
            "商城前台：http://127.0.0.1:19080\n    初始演示顾客账号：demo  密码：Lumo@2026",
            "商城管理端：http://127.0.0.1:19080/admin.html\n    初始演示管理员账号：admin  密码：Admin@2026",
            "接口联调台：http://127.0.0.1:29080/debug\n    初始演示顾客账号：demo  密码：Lumo@2026\n    初始演示管理员账号：admin  密码：Admin@2026",
            "Consul注册中心：http://127.0.0.1:18500/ui/\n    登录：默认无需账号密码",
            "Jaeger：http://127.0.0.1:26686\n    登录：默认无需账号密码",
            "RabbitMQ：http://127.0.0.1:25672\n    容器初始化账号：fixture-mq  密码：fixture-mq-password",
            "MinIO接口：http://127.0.0.1:19001\n    账号（Access Key）：fixture-minio  密码（Secret Key）：fixture-minio-password",
            "MinIO控制台：http://127.0.0.1:19002\n    账号（Access Key）：fixture-minio  密码（Secret Key）：fixture-minio-password",
        ):
            self.assertIn(expected, result.stdout)
        queried = {tuple(c["args"][-2:]) for c in self.calls() if self.command(c) == "exec"}
        self.assertEqual(
            queried,
            {
                ("printenv", key)
                for key in (
                    "DEMO_MODE",
                    "RABBITMQ_DEFAULT_USER",
                    "RABBITMQ_DEFAULT_PASS",
                    "MINIO_ROOT_USER",
                    "MINIO_ROOT_PASSWORD",
                )
            },
        )

    def test_startup_uses_running_credentials_and_prints_password_characters_literally(self):
        (self.project / ".env").write_text(
            self.template
            + "RABBITMQ_USER=unapplied-user\nRABBITMQ_PASSWORD=unapplied-password\nMINIO_ACCESS_KEY=unapplied-key\nMINIO_SECRET_KEY=unapplied-secret\n"
        )
        state = self.read_runtime()
        rabbit_password = r'50% = "quoted" \\ $(touch "$SIDE_EFFECT_FILE")'
        minio_password = r"another = 'quoted' \\ `touch \"$SIDE_EFFECT_FILE\"` %s"
        state["service_env"]["rabbitmq"] = {
            "RABBITMQ_DEFAULT_USER": "runtime-mq",
            "RABBITMQ_DEFAULT_PASS": rabbit_password,
        }
        state["service_env"]["minio"] = {"MINIO_ROOT_USER": "runtime-minio", "MINIO_ROOT_PASSWORD": minio_password}
        self.write_runtime(state)
        result = self.run_start("order")
        self.assert_success(result)
        self.assertIn("容器初始化账号：runtime-mq  密码：" + rabbit_password, result.stdout)
        self.assertIn("账号（Access Key）：runtime-minio  密码（Secret Key）：" + minio_password, result.stdout)
        self.assertNotIn("unapplied-", result.stdout)
        self.assertFalse(self.side_effect.exists())

    def test_startup_does_not_advertise_demo_defaults_when_demo_mode_is_disabled(self):
        for mode in ("false", "1"):
            with self.subTest(mode=mode):
                state = self.read_runtime()
                state["service_env"]["iam"]["DEMO_MODE"] = mode
                self.write_runtime(state)
                result = self.run_start("order")
                self.assert_success(result)
                self.assertNotIn("Lumo@2026", result.stdout)
                self.assertNotIn("Admin@2026", result.stdout)
                self.assertEqual(result.stdout.count("登录：使用已有账号（演示模式未启用）"), 3)

    def test_startup_reports_unavailable_credentials_without_inventing_defaults(self):
        state = self.read_runtime()
        state["service_env"] = {}
        self.write_runtime(state)
        result = self.run_start("order")
        self.assert_success(result)
        for expected in ("无法读取演示模式", "无法读取RabbitMQ账号密码", "无法读取MinIO账号密码"):
            self.assertIn(expected, result.stdout)
        for password in ("Lumo@2026", "Admin@2026", "fixture-mq-password", "fixture-minio-password"):
            self.assertNotIn(password, result.stdout)

    def test_failed_start_does_not_print_access_or_login_details(self):
        result = self.run_start("order", fail="app_up")
        self.assertNotEqual(result.returncode, 0)
        for text in ("可访问地址", "Lumo@2026", "Admin@2026", "fixture-mq-password", "fixture-minio-password"):
            self.assertNotIn(text, result.stdout)
        self.assertFalse(any(self.command(c) == "exec" for c in self.calls()))

    def test_env_creation_preservation_and_never_executing_shell_text(self):
        self.assert_success(self.run_start("order"))
        self.assertEqual((self.project / ".env").read_text(), self.template)
        original = 'CUSTOM_KEEP=original\nPROBE=$(touch "$SIDE_EFFECT_FILE")\n'
        (self.project / ".env").write_text(original)
        self.assert_success(self.run_start("order"))
        self.assertEqual((self.project / ".env").read_text(), original)
        self.assertFalse(self.side_effect.exists())

    def test_fixed_project_paths_support_unrelated_cwd_and_spaces(self):
        self.assert_success(self.run_start("order"))
        for call in self.calls():
            if call["args"][0] == "compose" and self.command(call) != "version" and "--help" not in call["args"]:
                a = call["args"]
                self.assertEqual(a[a.index("--project-directory") + 1], str(self.project))
                self.assertEqual(a[a.index("--env-file") + 1], str(self.project / ".env"))
                flag = "-f" if "-f" in a else "--file"
                self.assertEqual(a[a.index(flag) + 1], str(self.project / "compose.yaml"))

    def test_help_invalid_options_and_service_names_have_no_side_effects(self):
        result = self.run_start("--help", fail="compose")
        self.assertEqual(result.returncode, 0)
        self.assertIn("--no-build", result.stdout)
        self.assertIn("--update-infra", result.stdout)
        self.assertEqual(self.calls(), [])
        for args in (
            ("--wait-timeout",),
            ("--wait-timeout", "0"),
            ("--wait-timeout", "-1"),
            ("--wait-timeout", "abc"),
            ("--unknown-option",),
            ("unknown-service",),
        ):
            with self.subTest(args=args):
                self.clear_calls()
                result = self.run_start(*args)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.calls(), [])
                self.assertFalse((self.project / ".env").exists())

    def test_daemon_compose_wait_and_config_failures_never_build_or_start(self):
        for failure in ("daemon", "compose", "wait_support", "config"):
            with self.subTest(failure=failure):
                self.clear_calls()
                result = self.run_start("order", fail=failure)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.builds(), [])
                self.assertEqual(self.actual_ups(), [])


class DockerShutdownTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="lumo shutdown ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.project = self.root / "project with spaces"
        (self.project / "scripts").mkdir(parents=True)
        shutil.copy2(PROJECT / "scripts/down.sh", self.project / "scripts/down.sh")
        (self.project / "compose.yaml").write_text("name: lumo-mall\nservices: {}\n")
        self.record = self.root / "docker calls.json"
        docker = self.root / "docker"
        docker.write_text(
            "#!" + sys.executable + "\nimport json,os,sys\nfrom pathlib import Path\n"
            "Path(os.environ['TEST_DOCKER_RECORD']).write_text(json.dumps(sys.argv[1:]))\n"
        )
        docker.chmod(0o755)
        self.env = dict(
            os.environ,
            PATH=str(self.root) + os.pathsep + os.environ.get("PATH", ""),
            TEST_DOCKER_RECORD=str(self.record),
            COMPOSE_FILE=str(self.root / "other project.yaml"),
            COMPOSE_ENV_FILES=str(self.root / "other.env"),
        )

    def run_down(self):
        return subprocess.run(
            ["sh", str(self.project / "scripts/down.sh")],
            cwd=self.root,
            env=self.env,
            text=True,
            capture_output=True,
            timeout=10,
        )

    def test_shutdown_uses_startup_project_and_env_despite_external_compose_files(self):
        (self.project / ".env").write_text("COMPOSE_PROJECT_NAME=custom-lumo\n")
        result = self.run_down()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            json.loads(self.record.read_text()),
            [
                "compose",
                "--project-directory",
                str(self.project),
                "--env-file",
                str(self.project / ".env"),
                "-f",
                str(self.project / "compose.yaml"),
                "down",
            ],
        )

    def test_missing_startup_env_fails_without_stopping_any_containers(self):
        result = self.run_down()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(".env", result.stderr)
        self.assertFalse(self.record.exists())


if __name__ == "__main__":
    unittest.main()
