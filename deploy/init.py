#!/usr/bin/env python3
"""Generate fresh same-host secrets without reading any previous CRM config."""
import argparse, base64, os, secrets
from pathlib import Path

p=argparse.ArgumentParser()
p.add_argument('--directory',required=True)
p.add_argument('--domain',required=True)
p.add_argument('--profile',choices=['test2g','standard4g','standard8g'],default='test2g')
p.add_argument('--image',default='crm-standard:local')
a=p.parse_args()
dest=Path(a.directory);dest.mkdir(parents=True,exist_ok=True)
if (dest/'runtime.env').exists() or (dest/'.env').exists():raise SystemExit('Refusing to replace existing configuration')
os.umask(0o077)
token=lambda:secrets.token_hex(32)
key=lambda:base64.b64encode(secrets.token_bytes(32)).decode().rstrip('=')
password=secrets.token_urlsafe(24);db=token()
runtime={
'AICRM_DATABASE_URL':f'postgres://crm:{db}@db:5432/crm_standard?sslmode=disable',
'AICRM_LISTEN_ADDR':'0.0.0.0:8080','AICRM_PUBLIC_ORIGIN':f'https://{a.domain}',
'AICRM_RELEASE_SHA':a.image.split(':')[-1],'AICRM_ROLE':'api',
'AICRM_BOOTSTRAP_ENABLED':'true','AICRM_BOOTSTRAP_USERNAME':'admin','AICRM_BOOTSTRAP_PASSWORD':password,'AICRM_BOOTSTRAP_DISPLAY_NAME':'系统管理员',
'AICRM_SURVEY_DATA_KEY':key(),'AICRM_IDENTITY_PHONE_DATA_KEY':key(),
'AICRM_REFERRAL_TOKEN_DATA_KEY':key(),'AICRM_ORDER_CONTACT_DATA_KEY':key(),
'AICRM_COMMERCE_PUSH_PAYLOAD_DATA_KEY':key(),
'AICRM_WECOM_CONTEXT_SIGNING_KEY':token(),'AICRM_OPEN_PLATFORM_JWT_SIGNING_KEY':token(),
'AICRM_WORKER_LIMIT':'1','AICRM_OUTBOUND_PROVIDER_ENABLED':'false','AICRM_AI_GENERATION_ENABLED':'false','AICRM_STANDARD_PERIODIC_ENABLED':'false',
'AICRM_WECOM_ENABLED':'false','AICRM_WECOM_CUSTOMER_SYNC_ENABLED':'false','AICRM_WECOM_MESSAGE_ARCHIVE_ENABLED':'false','AICRM_WECHAT_PAY_PROVIDER_ENABLED':'false','AICRM_ALIPAY_PROVIDER_ENABLED':'false','AICRM_WECHAT_SHOP_PROVIDER_ENABLED':'false',
'AICRM_OPS_ENABLED':'false','AICRM_OPS_NOTIFICATION_ENABLED':'false','AICRM_OPS_RETENTION_ENABLED':'false',
}
sizes={
'test2g':{'DB_MEMORY':'512m','API_MEMORY':'640m','API_GO_MEMORY':'384MiB','DB_POOL_MAX':'5','PG_SHARED_BUFFERS':'128MB','PG_WORK_MEM':'2MB','PG_MAX_CONNECTIONS':'25'},
'standard4g':{'DB_MEMORY':'1024m','API_MEMORY':'1024m','API_GO_MEMORY':'640MiB','DB_POOL_MAX':'10','PG_SHARED_BUFFERS':'256MB','PG_WORK_MEM':'4MB','PG_MAX_CONNECTIONS':'40','WORKER_MEMORY':'768m'},
'standard8g':{'DB_MEMORY':'2048m','API_MEMORY':'2048m','API_GO_MEMORY':'1280MiB','DB_POOL_MAX':'15','PG_SHARED_BUFFERS':'512MB','PG_WORK_MEM':'4MB','PG_MAX_CONNECTIONS':'60','WORKER_MEMORY':'1536m'},
}
compose={'CRM_DOMAIN':a.domain,'CRM_IMAGE':a.image,'DB_PASSWORD':db,**sizes[a.profile]}
for name,data in [('runtime.env',runtime),('.env',compose)]:
 (dest/name).write_text(''.join(f'{k}={v}\n' for k,v in data.items()))
(dest/'administrator.txt').write_text(f'入口: https://{a.domain}\n账号: admin\n密码: {password}\n')
print(f'Created fresh {a.profile} configuration; credentials saved in administrator.txt')
