# 独立标准版测试环境交付

入口：**https://crm.qianlan333.cloud**  
主机：49.232.57.128，2 核 / 约 2GB 内存。安装目录：`/opt/crm-standard`。  
仓库：https://github.com/qianlan33333-png/AI-CRM-standard （private）。

管理员账号为 `admin`，密码保存在安装机 `/opt/crm-standard/administrator.txt`，本机交付副本位于仓库外发忽略目录 `.standard-secrets/crm-test-49.232.57.128/administrator.txt`。密码和运行密钥不进入 Git。一次性初始化已关闭，三项 bootstrap 字段已全部清空；管理员和加密密钥保留。应使用本次新密码，不沿用旧 CRM 凭据。

## 准确构件

- 运行源码 head：`7793baf4931fe805efce033412170dd16b614ce6`
- 运行源码 tree：`4ecccb3e0b20100602c5440a2b8526208afccfda`
- CRM image：`crm-standard:7793baf4931fe805efce033412170dd16b614ce6`
- 目标机 image manifest：`sha256:29444e84dcd322ccb308b80059b93c45ff7820cb4c89da4dff9d7075f70dd3c6`
- 应用二进制 SHA256：`ef11aab52df9bf34123713cde015fa4b0afa871727ac2883d9bfc9d9422fec6b`
- 本机完整同机镜像包 SHA256：`ce5c6693f326aab46b0db8121e4318b9ea031b7fe733431a73b95338206f04bf`
- 已上传应用修正包 SHA256：`3f26402aa98b3d0fb9f137c01481d6ce1c2316282e89f64e87e0993fd44e503d`
- 数据库 PostgreSQL **16.15 / amd64**，平台迁移 **0259（253 份迁移文件）**，River schema **6**。
- HTTPS Caddy **2.11.7**，公开受信任证书已签发，浏览器无安全告警且完成登录。

完整镜像包在本机忽略目录 `release/`，目标只接收和加载构件，不编译源码。最终仓库后续提交只补测试断言/交付文档，已安装运行版本以上述 head 为准。

## 清理与空库验证

旧 API/worker 和 4 个同步 timer 已停止；14 个旧 CRM unit 已 mask，旧命名空间日志服务停止。删除了 `/opt/aicrm`、`/etc/aicrm`、旧应用备份/构建/企业配置及家目录 aicrm 夹具，移除 PG18 业务 cluster 和临时 PG16 数据/运行目录。未修改操作系统、SSH、云主机 agent 或其他服务器。

新库只从迁移初始化，不复制旧库。完整逐表计数为 514 张表，其中 497 张为空，17 张非空仅为系统默认值、一个管理员、登录/运行审计、迁移及新运行配置应用元数据。客户、商品、订单、问卷/答卷、标签、渠道、策略、人群、任务及企业接入配置均为空。

`river_job=0`、`external_effects=0`、`admin_users=1`。只运行 db/api/proxy 三个容器；所有业务 Provider、模型、自动同步和定期生成关闭，无 worker，无旧 CRM active timer。PG 5432 不映射宿主机；API 18080 只绑定回环，公开入口为 80/443，SSH 22 保留。

## 实际验收

浏览器完成 HTTPS 登录，检查经营总览、客户、商品、订单、问卷、渠道、标签、运营周期及 AI 待审批任务的空状态。API 重建后会话仍可用，关闭 bootstrap 后 `/readyz` 返回上述准确源码 head。标签私有页面别名已补入独立构件并现场回读。截图见 `qa/deployed-empty-overview.png`，脱敏收据见 `receipts/`。

本机隔离环境的新增行为测试、完整历史、权限/重放/商品变化反例、内部说明保存刷新、Linux 构建、迁移和 Compose 重启持久性已通过。模型完成内容使用合成夹具。**没有执行真实模型、企微、支付或其他 Provider 调用；真实渠道/业务效果未验收。**

串行空库检查时容器内存约：API 138MiB、PG 100MiB、Caddy 14MiB；宿主机 available 约 1.0GB，根盘剩余约 25GB。此为该时间点的空闲样本，不代表生产容量。未来规格与启用说明见 [INSTALL.md](INSTALL.md)。
