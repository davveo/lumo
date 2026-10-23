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
DOMAINS = "iam member address catalog media interaction engagement search recommendation content analytics cart pricing checkout order campaign coupon points group flash inventory fulfillment logistics aftersale payment refund invoice reconciliation notification helpdesk".split()
APPS = DOMAINS + ["gateway"]
INFRA = ["mysql", "redis", "rabbitmq", "minio", "jaeger", "otel-collector"]
INIT = "storage-volume-init"
APP_IMAGE = "lumo-mall-backend:dev"
MINIO_IMAGE = "lumo-minio:release-20250907"

FAKE_DOCKER = r'''import hashlib,json,os,pathlib,re,sys
args=sys.argv[1:]
with open(os.environ['TEST_DOCKER_RECORD'],'a',encoding='utf-8') as stream:
    stream.write(json.dumps({'args':args,'cwd':os.getcwd()})+'\n')
path=pathlib.Path(os.environ['TEST_DOCKER_RUNTIME']);state=json.loads(path.read_text())
failure=os.environ.get('TEST_DOCKER_FAIL_STAGE','')
apps=state['domains']+['gateway'];infra=state['infra'];initializer='storage-volume-init';services=infra+[initializer]+apps
images={n:('lumo-mall-backend:dev' if n in apps else 'lumo-minio:release-20250907' if n=='minio' else 'fixture-'+n+':fixed') for n in services}
def save():path.write_text(json.dumps(state))
def die(message,code=1):print(message,file=sys.stderr);raise SystemExit(code)
def config_hash(name):return hashlib.sha256((name+'\0'+state.get('config_overrides',{}).get(name,'v1')).encode()).hexdigest()
def container(value):return next((c for n,c in state['containers'].items() if value in (n,c['id'],c.get('name',''))),None)
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
rest=args[1:];commands={'version','config','build','up','port','ps','logs','down','restart','start','wait'}
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
        for n in services:print('  '+n+':\n    image: '+images[n])
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
        print('\n'.join(state['containers'][n]['id'] for n in chosen if n in state['containers'] and (all_states or state['containers'][n]['status']=='running')))
    elif '--format' in tail:print(json.dumps([{'Service':n,'ID':state['containers'][n]['id'],'State':state['containers'][n]['status'],'Health':state['containers'][n]['health']} for n in chosen if n in state['containers']]))
elif command=='up':
    selected=[];skip=False
    for value in tail:
        if skip:skip=False;continue
        if value in {'--wait-timeout','--exit-code-from','--timeout','-t','--pull'}:skip=True;continue
        if value in services:selected.append(value)
    selected=selected or services[:];is_app=any(n in apps for n in selected)
    if failure=='up' or failure=='app_up' and is_app or failure=='infra_up' and not is_app:die('fixture health wait failure',23)
    for n in selected:
        old=state['containers'].get(n);image=state['images'].get(images[n],old['image'] if old else 'sha256:fixture-'+n)
        replace=not old or '--force-recreate' in tail or ('--no-recreate' not in tail and (old['image']!=image or old['config_hash']!=config_hash(n)))
        if replace:
            state['images'].setdefault(images[n],image)
            state['sequence']+=1;old={'id':'cid-'+n+'-'+str(state['sequence']),'name':'lumo-'+n,'image':image,'config_hash':config_hash(n)};state['containers'][n]=old
        old.update(status='exited' if n==initializer else 'running',health='none' if n in ('jaeger','otel-collector',initializer) else 'healthy',exit_code=0)
    save()
elif command=='port':
    ports={('gateway','8080'):'19080',('jaeger','16686'):'26686',('rabbitmq','15672'):'25672',('minio','9000'):'19001',('minio','9001'):'19002'}
    key=tuple([v for v in tail if not v.startswith('-')][-2:])
    if key not in ports:die('Unexpected port lookup '+repr(tail),92)
    if key[0] not in state['containers']:raise SystemExit(1)
    binding=state.get('port_bindings',{}).get(':'.join(key),'127.0.0.1:'+ports[key])
    if binding:print(binding)
elif command in ('down','restart','start'):die('Unexpected broad lifecycle operation '+command,93)
elif command=='wait':
    for n in tail:
        if n in state['containers']:print(state['containers'][n]['exit_code'])
'''

class DockerStartupTests(unittest.TestCase):
    maxDiff = None

    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(prefix="lumo incremental startup ");self.addCleanup(self.temp.cleanup)
        self.sandbox=Path(self.temp.name);self.project=self.sandbox/"project with spaces";self.project.mkdir()
        for rel in ("start.sh","start.command","scripts/up.sh","scripts/start-incremental.sh","scripts/source-fingerprint.sh","Dockerfile",".dockerignore","compose.yaml","deploy/minio.Dockerfile"):
            src=PROJECT/rel
            if not src.is_file():self.fail("Required startup source missing: "+rel)
            dest=self.project/rel;dest.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(src,dest)
        files={"go.mod":"module github.com/davveo/lumo\n\ngo 1.26\n","go.sum":"fixture v1\n","api/mall/v1/domain.proto":'syntax = "proto3";\n',
            "internal/platform/platform.go":"package platform\nconst Version = 1\n","internal/bootstrap/registry.go":"package bootstrap\n",
            "internal/democatalog/catalog.go":"package democatalog\n","internal/httpgateway/gateway.go":"package httpgateway\n",
            "internal/devstack/stack.go":"package devstack\n","cmd/server/main.go":"package main\nfunc main() {}\n",
            "cmd/gateway/main.go":"package main\nfunc main() {}\n","cmd/devstack/main.go":"package main\nfunc main() {}\n",
            "web/dist/index.html":"<title>Lumo test</title>","web/dist/admin.html":"<title>Lumo admin</title>","web/dist/app.js":"const app = 'v1';\n",
            "deploy/jaeger.yaml":"fixture: jaeger\n","deploy/otel-collector.yaml":"fixture: collector\n","README.md":"Fixture docs\n",
            ".env.example":"GATEWAY_PORT=19080\nCUSTOM_KEEP=template\n"}
        for n in DOMAINS:
            files[f"services/{n}/service.go"]=f"package {n}\n"
            files[f"services/{n}/internal/usecase/service.go"]="package usecase\nconst Policy = 1\n"
        for rel,text in files.items():
            p=self.project/rel;p.parent.mkdir(parents=True,exist_ok=True);p.write_text(text,encoding="utf-8")
        self.template=files[".env.example"];self.tools=self.sandbox/"fake tools";self.tools.mkdir()
        docker=self.tools/"docker";docker.write_text("#!"+sys.executable+"\n"+FAKE_DOCKER);docker.chmod(0o755)
        self.record=self.sandbox/"docker calls.jsonl";self.runtime=self.sandbox/"fake daemon.json";self.side_effect=self.sandbox/"env must not execute"
        images={APP_IMAGE:"sha256:original-app",MINIO_IMAGE:"sha256:original-minio"};containers={}
        for n in INFRA+[INIT]+APPS:
            image=images[APP_IMAGE] if n in APPS else images[MINIO_IMAGE] if n=="minio" else "sha256:fixture-"+n
            containers[n]={"id":"cid-"+n+"-original","name":"lumo-"+n,"status":"exited" if n==INIT else "running",
                "health":"none" if n in ("jaeger","otel-collector",INIT) else "healthy","exit_code":0,"image":image,
                "config_hash":hashlib.sha256((n+"\0v1").encode()).hexdigest()}
        self.write_runtime({"domains":DOMAINS,"infra":INFRA,"engine_id":"daemon-fixture","sequence":0,"images":images,"containers":containers,"config_overrides":{}})
        self.outside=self.sandbox/"unrelated working directory";self.outside.mkdir()
        self.env={k:v for k,v in os.environ.items() if k not in {"COMPOSE_FILE","COMPOSE_PROJECT_NAME","COMPOSE_ENV_FILES","DOCKER_HOST","DOCKER_CONTEXT","GATEWAY_PORT","JAEGER_PORT","RABBITMQ_UI_PORT","MINIO_API_PORT","MINIO_CONSOLE_PORT","WAIT_TIMEOUT","STARTUP_TIMEOUT"}}
        self.env.update(PATH=str(self.tools)+os.pathsep+os.environ.get("PATH",""),TEST_DOCKER_RECORD=str(self.record),
            TEST_DOCKER_RUNTIME=str(self.runtime),SIDE_EFFECT_FILE=str(self.side_effect),TERM="dumb")

    def read_runtime(self):return json.loads(self.runtime.read_text())
    def write_runtime(self,value):self.runtime.write_text(json.dumps(value))
    def mutate(self,rel,text="\n// changed runtime behavior\n"):
        p=self.project/rel;p.parent.mkdir(parents=True,exist_ok=True)
        with p.open("a") as stream:stream.write(text)
    def run_start(self,*args,entry="start.sh",fail="",extra_env=None):
        env=dict(self.env,TEST_DOCKER_FAIL_STAGE=fail);env.update(extra_env or {})
        return subprocess.run(["sh",str(self.project/entry),*args],cwd=self.outside,env=env,stdin=subprocess.DEVNULL,text=True,capture_output=True,timeout=90)
    def calls(self):
        return [json.loads(line) for line in self.record.read_text().splitlines()] if self.record.exists() else []
    def clear_calls(self):self.record.write_text("")
    def deployment_records(self):
        return {str(p.relative_to(self.project)):p.read_text() for p in (self.project/".local"/"start").glob("*/*.state") if p.stem in APPS}
    @staticmethod
    def command(call):
        args=call["args"]
        if args[0]!="compose":return " ".join(args[:2]) if args[0]=="image" else args[0]
        return next((v for v in args[1:] if v in {"version","config","build","up","port","ps","logs","down","restart","start","wait"}),"")
    @staticmethod
    def selected(call):
        command=DockerStartupTests.command(call)
        if command not in ("up","build"):return set()
        result=set();skip=False
        for value in call["args"][call["args"].index(command)+1:]:
            if skip:skip=False;continue
            if value in {"--wait-timeout","--exit-code-from","--timeout","-t","--pull"}:skip=True;continue
            if value in APPS+INFRA+[INIT]:result.add(value)
        return result
    def actual_ups(self):return [c for c in self.calls() if self.command(c)=="up" and "--help" not in c["args"]]
    def builds(self):return [c for c in self.calls() if self.command(c)=="build"]
    def app_targets(self):return set().union(*(self.selected(c)&set(APPS) for c in self.actual_ups())) if self.actual_ups() else set()
    def assert_success(self,result):
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        self.assertFalse(any(self.command(c) in {"down","restart","start"} for c in self.calls()),self.calls())
    def establish_baseline(self):self.assert_success(self.run_start());self.clear_calls()
    def assert_infra_preserved(self,before):
        after=self.read_runtime()["containers"]
        for n in INFRA+[INIT]:self.assertEqual(after[n],before["containers"][n],n)

    def test_first_run_updates_apps_once_and_preserves_running_infrastructure(self):
        before=self.read_runtime();self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(),set(APPS));self.assertEqual(len(self.builds()),1)
        self.assertNotIn("minio",self.selected(self.builds()[0]));self.assert_infra_preserved(before)
        for call in self.actual_ups():
            self.assertIn("--no-deps",call["args"]);self.assertIn("--no-build",call["args"]);self.assertTrue(self.selected(call))

    def test_repeated_run_without_changes_does_not_build_or_restart_anything(self):
        self.establish_baseline();before=self.read_runtime();self.assert_success(self.run_start())
        self.assertEqual(self.builds(),[]);self.assertEqual(self.actual_ups(),[]);self.assertEqual(self.read_runtime(),before)

    def test_order_and_frontend_updates_do_not_dirty_other_containers_via_shared_tag(self):
        self.establish_baseline();before=self.read_runtime();self.mutate("services/order/internal/usecase/service.go")
        self.assert_success(self.run_start());self.assertEqual(self.app_targets(),{"order"});self.assertEqual(len(self.builds()),1);self.assert_infra_preserved(before)
        state=self.read_runtime();self.assertNotEqual(state["containers"]["order"]["image"],state["containers"]["payment"]["image"])
        self.clear_calls();self.assert_success(self.run_start());self.assertEqual(self.actual_ups(),[]);self.assertEqual(self.builds(),[])
        self.mutate("web/dist/app.js");self.clear_calls();self.assert_success(self.run_start());self.assertEqual(self.app_targets(),{"gateway"})
        self.clear_calls();self.assert_success(self.run_start());self.assertEqual(self.actual_ups(),[])

    def test_shared_platform_updates_all_apps_and_preserves_infrastructure(self):
        self.establish_baseline();before=self.read_runtime();self.mutate("internal/platform/platform.go")
        self.assert_success(self.run_start());self.assertEqual(self.app_targets(),set(APPS));self.assertEqual(len(self.builds()),1);self.assert_infra_preserved(before)

    def test_demo_catalog_updates_only_catalog_and_inventory(self):
        self.establish_baseline();self.mutate("internal/democatalog/catalog.go");self.assert_success(self.run_start())
        self.assertEqual(self.app_targets(),{"catalog","inventory"})

    def test_documentation_tests_and_devstack_do_not_restart_runtime(self):
        self.establish_baseline()
        for p in ("README.md","services/order/internal/usecase/service_test.go","cmd/devstack/main.go","internal/devstack/stack.go"):self.mutate(p)
        self.assert_success(self.run_start());self.assertEqual(self.builds(),[]);self.assertEqual(self.actual_ups(),[])

    def test_changed_resolved_order_config_only_updates_order_without_source_build(self):
        self.establish_baseline();state=self.read_runtime();state["config_overrides"]["order"]="new-environment";self.write_runtime(state)
        self.assert_success(self.run_start());self.assertEqual(self.app_targets(),{"order"});self.assertEqual(self.builds(),[])

    def test_running_middleware_config_change_is_not_applied_implicitly(self):
        self.establish_baseline();state=self.read_runtime();before=json.loads(json.dumps(state));state["config_overrides"]["mysql"]="changed";self.write_runtime(state)
        self.assert_success(self.run_start());self.assertEqual(self.actual_ups(),[])
        self.assertEqual(self.read_runtime()["containers"]["mysql"],before["containers"]["mysql"])

    def test_explicit_infrastructure_update_applies_configuration(self):
        self.establish_baseline();before=self.read_runtime();state=json.loads(json.dumps(before))
        state["config_overrides"]["mysql"]="changed";self.write_runtime(state)
        self.assert_success(self.run_start("--update-infra"));self.assertEqual(self.app_targets(),set())
        self.assertEqual(set().union(*(self.selected(c) for c in self.actual_ups())),set(INFRA+[INIT]))
        for n in INFRA+[INIT]:self.assertNotEqual(self.read_runtime()["containers"][n]["id"],before["containers"][n]["id"],n)
        for n in APPS:self.assertEqual(self.read_runtime()["containers"][n],before["containers"][n],n)
        self.assertEqual([self.selected(c) for c in self.builds()],[{"minio"}])

    def test_running_minio_does_not_require_its_old_local_image_tag(self):
        state=self.read_runtime();del state["images"][MINIO_IMAGE];self.write_runtime(state);before=self.read_runtime()
        self.assert_success(self.run_start("order"));self.assert_infra_preserved(before)
        self.assertEqual([self.selected(c) for c in self.builds()],[{"gateway"}])

    def test_stopped_minio_restores_missing_image_tag_without_recreating_container_or_initializer(self):
        state=self.read_runtime();state["containers"]["minio"]["status"]="exited";del state["images"][MINIO_IMAGE]
        cid=state["containers"]["minio"]["id"];initializer=state["containers"][INIT];self.write_runtime(state)
        self.assert_success(self.run_start("order"));self.assertEqual(self.read_runtime()["containers"]["minio"]["id"],cid)
        self.assertEqual(self.read_runtime()["containers"][INIT],initializer)
        self.assertFalse(any(INIT in self.selected(c) for c in self.actual_ups()))
        # Compose resolves service images before it resumes stopped containers.
        self.assertEqual([self.selected(c) for c in self.builds()],[{"gateway"},{"minio"}])

    def test_stopped_mysql_starts_same_container_without_rebuild(self):
        self.establish_baseline();state=self.read_runtime();state["containers"]["mysql"]["status"]="exited";cid=state["containers"]["mysql"]["id"];self.write_runtime(state)
        self.assert_success(self.run_start());self.assertEqual(set().union(*(self.selected(c) for c in self.actual_ups())),{"mysql"})
        self.assertEqual(self.read_runtime()["containers"]["mysql"]["id"],cid);self.assertEqual(self.builds(),[])
        for call in self.actual_ups():self.assertIn("--no-recreate",call["args"])

    def test_stopped_unchanged_app_starts_same_container_without_rebuild(self):
        self.establish_baseline();state=self.read_runtime();state["containers"]["order"]["status"]="exited";cid=state["containers"]["order"]["id"];self.write_runtime(state)
        self.assert_success(self.run_start());self.assertEqual(self.app_targets(),{"order"})
        self.assertEqual(self.read_runtime()["containers"]["order"]["id"],cid);self.assertEqual(self.builds(),[])
        for call in self.actual_ups():self.assertIn("--no-recreate",call["args"])

    def test_first_explicit_service_does_not_update_other_apps(self):
        before=self.read_runtime();self.assert_success(self.run_start("order"));self.assertEqual(self.app_targets(),{"order"})
        for n in set(APPS)-{"order"}:self.assertEqual(self.read_runtime()["containers"][n],before["containers"][n],n)
        self.assert_infra_preserved(before)

    def test_baselines_are_isolated_by_docker_engine_and_resolved_project_name(self):
        self.assert_success(self.run_start("order"));self.clear_calls()
        state=self.read_runtime();state["engine_id"]="another-daemon";self.write_runtime(state)
        self.assert_success(self.run_start("order"));self.assertEqual(len(self.builds()),1);self.clear_calls()
        state=self.read_runtime();state["project_name"]="another-project";self.write_runtime(state)
        self.assert_success(self.run_start("order"));self.assertEqual(len(self.builds()),1)
        baselines=list((self.project/".local"/"start").glob("*/order.state"));self.assertEqual(len(baselines),3)
        for path in baselines:self.assertEqual(len(path.read_text().splitlines()),3)

    def test_no_build_leaves_dirty_source_pending_for_next_normal_start(self):
        self.establish_baseline();self.mutate("services/order/internal/usecase/service.go")
        self.assert_success(self.run_start("--no-build"));self.assertEqual(self.builds(),[])
        self.clear_calls();self.assert_success(self.run_start());self.assertEqual(self.app_targets(),{"order"});self.assertEqual(len(self.builds()),1)

    def test_build_failure_does_not_advance_deployment_baseline(self):
        self.establish_baseline();self.mutate("services/order/internal/usecase/service.go")
        before=self.deployment_records()
        result=self.run_start(fail="build");self.assertNotEqual(result.returncode,0);self.assertEqual(self.actual_ups(),[]);self.assertNotIn("启动完成",result.stdout)
        self.assertEqual(self.deployment_records(),before)
        self.clear_calls();self.assert_success(self.run_start());self.assertEqual(self.app_targets(),{"order"});self.assertEqual(len(self.builds()),1)

    def test_health_failure_does_not_mark_changed_code_deployed(self):
        self.establish_baseline();self.mutate("services/order/internal/usecase/service.go")
        before=self.deployment_records()
        result=self.run_start(fail="app_up");self.assertNotEqual(result.returncode,0);self.assertNotIn("启动完成",result.stdout)
        self.assertEqual(self.deployment_records(),before)
        self.clear_calls();self.assert_success(self.run_start());self.assertEqual(self.app_targets(),{"order"})

    def test_edit_during_build_is_not_misrecorded_as_deployed(self):
        self.establish_baseline();self.mutate("services/order/internal/usecase/service.go")
        before=self.deployment_records()
        result=self.run_start(extra_env={"TEST_DOCKER_MUTATE_ON_BUILD":"services/order/internal/usecase/service.go"})
        self.assertNotEqual(result.returncode,0);self.assertEqual(self.app_targets(),set())
        self.assertEqual(self.deployment_records(),before)
        self.clear_calls();self.assert_success(self.run_start());self.assertEqual(self.app_targets(),{"order"})

    def test_fresh_install_runs_initializer_once_and_starts_dependencies(self):
        state=self.read_runtime();state["containers"]={};state["images"]={};self.write_runtime(state);self.assert_success(self.run_start())
        self.assertEqual(set().union(*(self.selected(c) for c in self.actual_ups())),set(APPS+INFRA+[INIT]))
        self.assertEqual(sum(INIT in self.selected(c) for c in self.actual_ups()),1)
        self.clear_calls();self.assert_success(self.run_start());self.assertEqual(self.actual_ups(),[])

    def test_dynamic_ports_and_legacy_mac_wrappers_still_work(self):
        for entry in ("start.sh","scripts/up.sh","start.command"):
            with self.subTest(entry=entry):
                self.clear_calls();result=self.run_start("order","--wait-timeout","71",entry=entry);self.assert_success(result)
                for port in ("19080","26686","25672","19001","19002"):self.assertIn(":"+port,result.stdout)
                self.assertIn("/admin.html",result.stdout);self.assertNotIn("http://localhost:8080",result.stdout)
                self.assertIn("可访问地址（在Docker宿主机浏览器打开）：",result.stdout)
                for line in result.stdout.splitlines():
                    if "http://" in line:self.assertTrue(line.startswith("  "),line)
                for call in self.actual_ups():
                    if "--wait-timeout" in call["args"]:self.assertEqual(call["args"][call["args"].index("--wait-timeout")+1],"71")

    def test_accessible_addresses_normalize_wildcards_use_first_binding_and_skip_empty(self):
        state=self.read_runtime();state["port_bindings"]={
            "gateway:8080":"0.0.0.0:19080\n[::]:19080",
            "jaeger:16686":"[::]:26686\n0.0.0.0:26686",
            "rabbitmq:15672":"192.0.2.27:25672",
            "minio:9000":""}
        self.write_runtime(state);result=self.run_start("order");self.assert_success(result)
        self.assertIn("可访问地址（在Docker宿主机浏览器打开）：",result.stdout)
        for expected in ("商城前台：http://127.0.0.1:19080","商城管理端：http://127.0.0.1:19080/admin.html",
                "接口联调台：http://127.0.0.1:19080/debug","Jaeger：http://[::1]:26686",
                "RabbitMQ：http://192.0.2.27:25672","MinIO控制台：http://127.0.0.1:19002"):
            self.assertIn("  "+expected,result.stdout)
        self.assertNotIn("0.0.0.0",result.stdout);self.assertNotIn("[::]:",result.stdout)
        self.assertNotIn("MinIO接口：",result.stdout)
        links=[line for line in result.stdout.splitlines() if "http://" in line]
        self.assertEqual(len(links),6)
        for line in links:self.assertTrue(line.startswith("  "),line)

    def test_env_creation_preservation_and_never_executing_shell_text(self):
        self.assert_success(self.run_start("order"));self.assertEqual((self.project/".env").read_text(),self.template)
        original='CUSTOM_KEEP=original\nPROBE=$(touch "$SIDE_EFFECT_FILE")\n';(self.project/".env").write_text(original)
        self.assert_success(self.run_start("order"));self.assertEqual((self.project/".env").read_text(),original);self.assertFalse(self.side_effect.exists())

    def test_fixed_project_paths_support_unrelated_cwd_and_spaces(self):
        self.assert_success(self.run_start("order"))
        for call in self.calls():
            if call["args"][0]=="compose" and self.command(call)!="version" and "--help" not in call["args"]:
                a=call["args"];self.assertEqual(a[a.index("--project-directory")+1],str(self.project))
                self.assertEqual(a[a.index("--env-file")+1],str(self.project/".env"))
                flag="-f" if "-f" in a else "--file";self.assertEqual(a[a.index(flag)+1],str(self.project/"compose.yaml"))

    def test_help_invalid_options_and_service_names_have_no_side_effects(self):
        result=self.run_start("--help",fail="compose");self.assertEqual(result.returncode,0);self.assertIn("--no-build",result.stdout);self.assertIn("--update-infra",result.stdout);self.assertEqual(self.calls(),[])
        for args in (("--wait-timeout",),("--wait-timeout","0"),("--wait-timeout","-1"),("--wait-timeout","abc"),("--unknown-option",),("unknown-service",)):
            with self.subTest(args=args):
                self.clear_calls();result=self.run_start(*args);self.assertNotEqual(result.returncode,0);self.assertEqual(self.calls(),[]);self.assertFalse((self.project/".env").exists())

    def test_daemon_compose_wait_and_config_failures_never_build_or_start(self):
        for failure in ("daemon","compose","wait_support","config"):
            with self.subTest(failure=failure):
                self.clear_calls();result=self.run_start("order",fail=failure);self.assertNotEqual(result.returncode,0);self.assertEqual(self.builds(),[]);self.assertEqual(self.actual_ups(),[])

if __name__ == "__main__":unittest.main()

