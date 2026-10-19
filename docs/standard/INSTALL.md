# 同机安装与维护

本仓库只服务独立企业环境。当前授权测试目标为 `49.232.57.128`，域名 `crm.qianlan333.cloud`；不使用原 CRM 的预发、正式机或发布队列。

## 构建

准备 Node 24、npm 11、Go 1.26.9、Docker Engine/Compose v2 和 buildx。本机运行 `npm ci --ignore-scripts`、`npm ci --prefix web/v3 --ignore-scripts`，提交准确源码后执行：

```sh
scripts/build-standard.sh
```

脚本物化已冻结的前端、在本机 Linux builder 编译 Linux/amd64 静态程序，保存 CRM/PostgreSQL/Caddy 镜像和 SHA256 文件。目标机不编译。Excel 可选镜像单独构建：

```sh
docker build --platform linux/amd64 -f deploy/Excel.Dockerfile -t crm-standard-excel:local .
```

## 全新安装

目标需 Docker 和 Compose，80/443 端口可用，域名已解析。安装目录放 `compose.yaml`、`Caddyfile`、`init.py`，导入构件：

```sh
gzip -dc crm-standard-SHA.tar.gz | docker load
python3 init.py --directory . --domain crm.qianlan333.cloud --profile test2g --image crm-standard:SHA
docker compose up -d db
docker compose run --rm migrate
docker compose up -d api proxy
docker compose ps
```

`init.py` 拒绝覆盖已有配置，随机生成独立密码和加密密钥。`runtime.env`、`.env`、`administrator.txt` 只保存在安装机的受限目录，不提交仓库。登录信息在 `administrator.txt`。管理员建立后把 `runtime.env` 的 `AICRM_BOOTSTRAP_ENABLED` 改为 `false`，再 `docker compose up -d api`。

数据库只在 Compose 网络内访问，没有宿主机 5432 映射。API 的 18080 只绑定宿主机回环；Caddy 提供 HTTPS 并持久化证书。数据库、应用文件、证书、Excel SQLite 都用独立卷；`docker compose down` 保留数据，禁止把 `down -v` 当作普通升级操作。

## 规格

|配置|数据库内存|API 内存|API Go 预算|连接池|PG 连接上限|PG shared_buffers/work_mem|
|---|---:|---:|---:|---:|---:|---|
|test2g|512MB|640MB|384MiB|5|25|128MB / 2MB|
|standard4g|1024MB|1024MB|640MiB|10|40|256MB / 4MB|
|standard8g|2048MB|2048MB|1280MiB|15|60|512MB / 4MB|

4GB/8GB 的 worker 预算分别建议 768MB/1536MB，每队列并发从 1 开始。Excel 预留 256MB。上述为配置起点，需按实际数据和任务测试。调整现有 `.env` 的资源变量即可，**不要为升级规格重新生成加密密钥**。API 和数据库预算之外保留系统/文件缓存空间。

test2g 只启动 db/api/proxy。默认所有 Provider、模型、同步和定期执行关闭，没有 worker。模型使用企业自己配置的外部 API，不部署本地模型。

## 启用资料整理与定期生成

先配置企业身份/员工范围及五类数据，维护商品主档和可选内部运营说明。配置现有人群包、发布动态 Agent、绑定发送人和可靠业务事实条件；人群的刷新继续使用现有刷新设置。不要使用尚未生成的 AI 摘要作为首次入群的唯一条件。

通过现有模型配置页填写外部 API；按部署配置启用 `AICRM_OUTBOUND_PROVIDER_ENABLED` 和 `AICRM_AI_GENERATION_ENABLED`。在原“运营闭环”页面保存定期策略草稿，选择人群包和间隔，确认后启用。需要后台定期生成时再设置 `AICRM_STANDARD_PERIODIC_ENABLED=true` 并启动：

```sh
docker compose --profile worker up -d worker
```

启用后先准备一次，随后按 UTC 纪元对齐（北京时间每日 08:00、每周四 08:00 或整点）。只复用现有动态生成的人工审核路径；不会直接批准或发送。关闭 worker 停止后台执行，暂停策略停止下一次发生。权限被撤销的创建人不再触发新任务。同次发生的冻结命令在重启后保持不变。

侧边栏读取只读；点击“整理文字资料”才补读历史并准备变化片段。摘要保留来源、时间和覆盖范围；未完成或预算外信息必须按未知处理。模型结果不保证语义正确，使用前仍需人工核实。辅助话术不会自动发送。

## Excel 可选组件

企业实际需要 Excel 批次时提供自己的 `excel-config.json` 与 `excel.env`（后者含随机 `EXCEL_BATCH_TOKEN`），并设置 API 的 `EXCEL_BATCH_URL=http://127.0.0.1:8791` 和相同 Token。`--profile excel` 的组件共享 API 的网络命名空间，只监听回环；SQLite 在 `excel_data` 卷。它不会独立获得客户归属或审批/发送权限。

## 升级、备份与恢复

普通升级导入已验证镜像，更新 `.env` 的 `CRM_IMAGE`，执行迁移后重建 api/worker；迁移前备份。保留原运行密钥、PG 卷和文件卷；迁移失败先只读核对，不盲目降版。

手动备份 PostgreSQL：`docker compose exec -T db pg_dump -U crm -d crm_standard -Fc > crm.dump`。同时安全保存运行密钥、app_data 和可选 excel_data；备份 Excel 时先停止 Excel 进程保证 SQLite 一致。恢复使用 PostgreSQL 16 新卷和 `pg_restore`，恢复相同加密密钥，再启动相同或兼容版本。备份含企业数据，不放 Git。

技术验收：HTTPS、`/readyz`、管理员登录、主要空页面、业务/接入配置计数、容器及 timer 状态、重启后持久性。模型、企微、支付等真实 Provider 验收单独记录；2GB 串行测试不代表生产容量。
