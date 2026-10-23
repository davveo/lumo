"""Regenerate the committed Compose file; not required to run the application."""
from pathlib import Path
import yaml

root=Path(__file__).resolve().parents[1]
names='iam member address catalog media interaction engagement search recommendation content analytics cart pricing checkout order campaign coupon points group flash inventory fulfillment logistics aftersale payment refund invoice reconciliation notification helpdesk'.split()
environment={
 'RPC_ADDR':':9000','DB_DRIVER':'mysql','REDIS_ADDR':'redis:6379',
 'AMQP_URL':'amqp://${RABBITMQ_USER:-mall}:${RABBITMQ_PASSWORD:-lumo_mq_local_2026}@rabbitmq:5672/',
 'OTEL_EXPORTER_OTLP_ENDPOINT':'otel-collector:4317','OTEL_TRACES_SAMPLER_ARG':'${OTEL_TRACES_SAMPLER_ARG:-1}',
 'INTERNAL_RPC_TOKEN':'${INTERNAL_RPC_TOKEN:-lumo_rpc_local_only_change_before_shared_deploy_2026}',
 'DEMO_MODE':'${DEMO_MODE:-true}','APP_ENV':'development',
}
deps={k:{'condition':'service_healthy'} for k in ['mysql','redis','rabbitmq']}
deps['otel-collector']={'condition':'service_started'}
services={
 'mysql':{'image':'mysql:8.4.4','environment':{'MYSQL_ROOT_PASSWORD':'${MYSQL_ROOT_PASSWORD:-lumo_root_local_2026}','MALL_DB_PASSWORD':'${MALL_DB_PASSWORD:-lumo_db_local_2026}'},'command':['--max-connections=600','--innodb-buffer-pool-size=256M'],'volumes':['mysql-data:/var/lib/mysql','./deploy/mysql:/docker-entrypoint-initdb.d:ro'],'healthcheck':{'test':['CMD-SHELL','MYSQL_PWD=$$MYSQL_ROOT_PASSWORD mysqladmin ping -h 127.0.0.1 -u root --silent'],'interval':'5s','timeout':'3s','retries':40,'start_period':'25s'},'restart':'unless-stopped'},
 'redis':{'image':'redis:7.4.2-alpine','command':['redis-server','--appendonly','yes'],'volumes':['redis-data:/data'],'healthcheck':{'test':['CMD','redis-cli','ping'],'interval':'5s','timeout':'3s','retries':20},'restart':'unless-stopped'},
 'rabbitmq':{'image':'rabbitmq:4.0.7-management-alpine','environment':{'RABBITMQ_DEFAULT_USER':'${RABBITMQ_USER:-mall}','RABBITMQ_DEFAULT_PASS':'${RABBITMQ_PASSWORD:-lumo_mq_local_2026}'},'ports':['127.0.0.1:${RABBITMQ_UI_PORT:-15672}:15672'],'volumes':['rabbitmq-data:/var/lib/rabbitmq'],'healthcheck':{'test':['CMD','rabbitmq-diagnostics','-q','ping'],'interval':'5s','timeout':'5s','retries':30,'start_period':'10s'},'restart':'unless-stopped'},
 'storage-volume-init':{'image':'alpine:3.22.1','user':'0:0','command':['sh','-ec','mkdir -p /var/jaeger/keys /var/jaeger/values /data; chown -R 10001:10001 /var/jaeger /data'],'volumes':['jaeger-data:/var/jaeger','minio-data:/data'],'restart':'no'},
 'jaeger':{'image':'jaegertracing/jaeger:2.5.0','user':'10001:10001','command':['--config=/etc/jaeger/config.yaml'],'volumes':['./deploy/jaeger.yaml:/etc/jaeger/config.yaml:ro','jaeger-data:/var/jaeger'],'ports':['127.0.0.1:${JAEGER_PORT:-16686}:16686'],'depends_on':{'storage-volume-init':{'condition':'service_completed_successfully'}},'restart':'unless-stopped'},
 'minio':{'image':'lumo-minio:release-20250907','build':{'context':'.','dockerfile':'deploy/minio.Dockerfile'},'user':'10001:10001','environment':{'MINIO_ROOT_USER':'${MINIO_ACCESS_KEY:-lumo_minio_local}','MINIO_ROOT_PASSWORD':'${MINIO_SECRET_KEY:-lumo_minio_local_secret_2026}','MINIO_API_CORS_ALLOW_ORIGIN':'${MINIO_CORS_ORIGINS:-http://localhost:${GATEWAY_PORT:-8080},http://127.0.0.1:${GATEWAY_PORT:-8080}}'},'volumes':['minio-data:/data'],'ports':['127.0.0.1:${MINIO_API_PORT:-9001}:9000','127.0.0.1:${MINIO_CONSOLE_PORT:-9002}:9001'],'healthcheck':{'test':['CMD','wget','-q','-O','/dev/null','http://127.0.0.1:9000/minio/health/ready'],'interval':'5s','timeout':'3s','retries':40,'start_period':'15s'},'depends_on':{'storage-volume-init':{'condition':'service_completed_successfully'}},'restart':'unless-stopped'},
 'otel-collector':{'image':'ghcr.io/open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib:0.124.1','command':['--config=/etc/otel/config.yaml'],'volumes':['./deploy/otel-collector.yaml:/etc/otel/config.yaml:ro'],'depends_on':{'jaeger':{'condition':'service_started'}},'restart':'unless-stopped'},
}
for n in names:
 env=dict(environment,SERVICE_NAME=n,DB_DSN=f'{n}:${{MALL_DB_PASSWORD:-lumo_db_local_2026}}@tcp(mysql:3306)/lumo_{n}?charset=utf8mb4&parseTime=True&loc=UTC&timeout=5s&readTimeout=10s&writeTimeout=10s')
 services[n]={'image':'lumo-mall-backend:dev','build':{'context':'.'},'environment':env,'depends_on':deps.copy(),'healthcheck':{'test':['CMD','/app/server','healthcheck'],'interval':'10s','timeout':'5s','retries':30,'start_period':'20s'},'read_only':True,'tmpfs':['/tmp:size=16m'],'stop_grace_period':'20s','restart':'unless-stopped'}
 if n=='media':
  env.update({'MINIO_ENDPOINT':'minio:9000','MINIO_PUBLIC_ENDPOINT':'${MINIO_PUBLIC_ENDPOINT:-localhost:${MINIO_API_PORT:-9001}}','MINIO_ACCESS_KEY':'${MINIO_ACCESS_KEY:-lumo_minio_local}','MINIO_SECRET_KEY':'${MINIO_SECRET_KEY:-lumo_minio_local_secret_2026}','MINIO_BUCKET':'lumo-media','MINIO_USE_SSL':'false'})
  services[n]['depends_on']['minio']={'condition':'service_healthy'}
gateway_env=dict(environment,HTTP_ADDR=':8080',CORS_ALLOWED_ORIGINS='${CORS_ALLOWED_ORIGINS:-}')
gateway_env.pop('DB_DRIVER',None)
services['gateway']={'image':'lumo-mall-backend:dev','build':{'context':'.'},'entrypoint':['/app/gateway'],'environment':gateway_env,'ports':['127.0.0.1:${GATEWAY_PORT:-8080}:8080'],'depends_on':{n:{'condition':'service_healthy'} for n in names},'healthcheck':{'test':['CMD','/app/gateway','healthcheck'],'interval':'10s','timeout':'8s','retries':20,'start_period':'10s'},'read_only':True,'stop_grace_period':'20s','restart':'unless-stopped'}
config={'name':'lumo-mall','services':services,'volumes':{n:{} for n in ['mysql-data','redis-data','rabbitmq-data','jaeger-data','minio-data']}}
(root/'compose.yaml').write_text('# Generated by deploy/generate_compose.py; all 30 domain services start by default.\n'+yaml.safe_dump(config,allow_unicode=True,sort_keys=False),encoding='utf-8')
print(f'compose.yaml: {len(names)} domain services + gateway + dependencies')
