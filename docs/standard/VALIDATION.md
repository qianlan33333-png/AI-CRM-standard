# 标准版候选验证

## 已执行

- 新建隔离 PostgreSQL 16，从零执行 River 与全部平台迁移（至 0259），成功。
- `TestStandardPostgreSQLHistoryScopeReceiptsAndProductNotes`：276 条有来源资料（250 聊天、25 问卷、1 人工备注），7 个模型片段任务；再次准备复用相同 River/EER 收据；完成投影可持久读取；员工缓存/任务隔离；新增文字保留且复用未变片段；当前商品价格/停用；订单不可用不作为无购买；私有说明 CAS/重放/公共 DTO 隔离；Native 策略 HTTP 保存及防伪造 actor。模型完成结果是合成夹具，并非真实模型效果。
- `TestCustomerAuthorshipFiltersBeforePaginationPostgreSQL`：真实 SQL 在分页前按客户作者、员工、会话类型和身份范围筛选。
- `TestCycleGenerationReusesFrozenOccurrenceAfterInterruptionAndHonorsPauseAndRevocation`：中断后复用原发生和冻结命令，暂停/撤权不创建新运行，新的预定发生独立接受。
- `TestAISuggestionPendingPollKeepsReceiptKeyAndRejectsChangedSnapshot` 以及现有 Sidebar AI 测试：异步轮询保持键，商品/上下文变化后拒绝旧建议，当前查看者变化和范围错误拒绝。
- customer/app、automation/app/provider、sidebar、product/app/http、operationcycle/app/http、platform/config/jobqueue 的相关测试运行；不同环境要求的旧浏览器/已安装 Provider 测试可能有其明确 skip，未将 skip 算作验收。相关 PostgreSQL 及新具名用例单独运行通过。
- 前端构建和 host 构建；商品重复保存/外推部分失败恢复 Node 合同通过。浏览器内部说明保存刷新、无外部配置的商品创建、空人群策略入口通过。
- 本机 Linux/amd64 镜像构建成功；Compose 全新数据库/API 健康。初始 customers/products/orders/river_job 均为 0；数据库 16.15。最终准确版本与目标收据见交付记录。

## 边界与反例结论

来源记录属于原模块，Customer 只保存派生文字资料；订单、商品、标签、渠道每次读取当前 Port。员工范围包含在缓存和任务键中，员工话语不作为客户意愿证据。摘要输出拒绝不存在的来源引用；过期片段不能覆盖当前投影。较长输入保留完整 owner 投影，单次模型输入明确完整/省略状态，优先最新原文与人工备注。人工审核仍是必要业务步骤。

Native 定期路径仅桥接现有策略、人群包、动态生成及审核，不具备发送接口。旧自动化的独立发送能力保留其原有开关/权限保护。test2g 不启动任何 worker 或 Provider。

## 未验证

外部模型的实际语义质量、真实企微/支付等 Provider、真实员工侧边栏登录和企业数据接入，均未配置及执行。2GB 仅做串行测试，不代表生产容量。没有对完整通用 CRM 做全仓/所有真实业务旅程的回归宣称。

## 源码与凭据检查

从指定 V4 tree 新导出，删除旧部署工作流、旧发布控制脚本、旧预发夹具与企业治理收据；新历史不包含旧 Git 历史。运行配置和密钥全部新生成且忽略。Gitleaks 文件扫描的命中来自合成测试密钥、生成客户端的 token 引用，以及被忽略的本次本机运行凭据；运行凭据未纳入候选。保留的 commerce-history 约 2KB 是合成测试夹具，不是企业同步快照。发布时再以 Git 跟踪文件清单确认未含 runtime.env、私钥、dump 或同步导出。
