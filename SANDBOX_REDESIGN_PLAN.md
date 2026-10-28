# Sandbox 改造实施计划（恢复版，2026-10-28）

说明：共享工作区在本轮中途被外部操作重置，原先未纳入 Git 的长篇历史计划文件丢失。本文件根据当前代码、服务器状态和真实节点验收报告重建关键状态；旧文档的逐字历史没有伪造恢复。

## 已实现的开发灰度路径

1. 配置由活动 SQL policy revision 管理：origin 路由、OCI image digest、网络拒绝、CPU/内存/PID/超时/输出/临时空间/产物上限、节点池与凭据引用。密钥在独立私有文件，不能进入 DB、Skill 包或归档。
2. 对仓库中发布的 Skill 生成不可变内容哈希制品。Broker 在一次任务准备时固定 digest，Node 缓存未命中按需拉取、验证后只读挂入容器；工作区快照只在 prepare 发送一次，多命令复用同一租约。
3. Node HMAC 签名协议和持久 nonce 防重放；journal 原子写入及目录同步、任务独占文件锁、启动时清理标记容器和孤儿目录、取消旧租约。失败拒绝接单，不回退旧 runner。
4. 独立进程中的 OCI 容器会话串行处理多条命令，输入/Skill 只读，工作区有界 tmpfs，网络禁止、非 root、资源限制。容器存活时受限导出 outputs 归档并计算 SHA256，收集后禁止新命令，失败清理并标记不确定状态。
5. AgentAPI 仅对配置的 canary 用户执行远程 Skill 逻辑，接收产物通过 ArtifactService 保存；非 canary 保留旧路径。现网 canary 用户是假身份，不存在真实聊天切流。

## 当前服务器状态与验收

详见 SANDBOX_REAL_NODE_REPORT_2026-10-28.md。31 节点与 155 API 已完成真实跨机、假数据、单槽测试；31 节点 SSH 反向隧道从 155 的私有制品接口拉取 Skill。活动 DB policy revision 4，Node 预期版本 4。31 的公网 1Panel 42644 风险仍在，用户先前明确选择暂不关闭，因此不得推送真实用户数据。

## 剩余切流门槛

- 完成 1Panel 公网管理面及 Docker 组高权限控制面安全评审，验证宿主机 secrets、Docker socket、metadata、内部网络/公网、其他租户目录、恶意后台进程与逃逸；不得仅凭 fake BusyBox 烟测批准真实用户任务。
- 为 Office/Python/Node Skill 预置镜像依赖并固定 digest，对 DOCX/XLSX/PPTX/PDF 输出、磁盘/内存峰值和多 Agent 并发容量逐项验收。当前 2 vCPU/3.8 GiB Node 仅允许单槽开发任务。
- 真实登录测试账号从聊天发起 Skill 调用，验证 prompt !-block、scoped Bash、长任务取消、Artifact Service 的生产对象存储与可见下载；严格限制可执行 Skill、灰度用户和配额。
- 持久化交付队列、断线重试与幂等结果、长期 tombstone 压缩与多实例 fencing 尚需实现；journal 满额会拒绝新任务。不能用当前内存结果缓存替代生产级投递保证。
- 将现场薄层 AgentAPI 镜像替换为推送代码后 CI 发布镜像，并通过同样的远端回归和灰度；回滚前先停 canary 路由，不删除审计记录。native 通道和外部 Skill 来源认证仍未开放。
