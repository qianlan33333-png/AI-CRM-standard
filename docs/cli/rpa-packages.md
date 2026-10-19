# 本地 Agent 群运营任务包

本地 Agent 使用现有 CLI 查询计划详情和已绑定群，以准确 `revision` 和官方群 ID 编写一个 Markdown。不要让用户记 ID，不以群名搜索代替 ID；同名/改名群使用原资产引用。先用 `aicrm-cli capabilities list` / `schema get` 发现当前注册查询命令。

```markdown
---
schema: aicrm.group-sequence.v1
plan_id: 16
plan_revision: 7
groups:
  - 官方群ID1
  - 官方群ID2
messages:
  - id: intro
    kind: text
    text: |
      第一条消息
      第二行
  - id: image-1
    kind: image
    relative_path: 1.jpg
  - id: document-1
    kind: file
    relative_path: 验收清单.docx
---
# 内部双群测试
每群发完整个序列，再进入下一个群。
```

将任务 Markdown 和素材放在助手已授权的同一目录。Agent 执行：

```sh
aicrm-cli group-ops rpa-package save --input /本地素材目录/task.md --idempotency-key 本次逻辑保存的固定键
aicrm-cli group-ops rpa-package get --query plan_id=16
```

CLI 在本机校验引用文件并补全大小、SHA256，保存回同一个 Markdown；发送给云端的只有 Markdown，不含文件字节。已有摘要不符、越界或符号链接逃出目录会拒绝。正文只供人和 AI 阅读，不执行脚本。保存创建冻结的暂停任务；0.2及后续助手的一次启用只绑定当前包，完成自动暂停；新包需要重新启用。首条发送前本地校验并冻结全部素材。与助手使用同一目录，相对路径不能随意改。

重复请求保持原幂等键；收到结果不明时先 get 核对原包，不能另换键重发。尚未发送的旧步骤可被新保存原子替换；已有发送授权/收据的未结束任务会拒绝替换，Agent 必须先核对进度。结果未知不能自动取消重发。任务包属于当前计划负责人，设备令牌不能写包；CLI 沿用独立终端登录与原权限。

云端只读显示原 Markdown 和实时执行结果，不再提供 RPA 配置表单。外部认证、企微当前群 ID、账号核验和 Outbound/EER 逐条记录仍由原执行链处理。
