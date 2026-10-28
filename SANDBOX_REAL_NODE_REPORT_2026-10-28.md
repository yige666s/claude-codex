# 2026-10-28 真实 Node 开发灰度联调报告

## 2026-10-28 补充：单账号真实聊天路由已开启（受限范围）

- 155 AgentAPI 已改为现场薄层镜像 claude-codex-agentapi:sandbox-realchat-20261028，活动 DB policy revision 5；31 Node 预期 revision 5，采用固定 digest 的 AgentAPI 运行时镜像（Python、Node、LibreOffice）。策略限额为 1 并发、768 MiB 内存、128 MiB 临时工作区、8 MiB 总产物、180 秒任务超时。
- 已将邮箱 yige29805@gmail.com 对应的一个 active 用户 ID 加入真实远程灰度名单。该账号触发 **explain 或 docx** 时，Skill prepare、脚本执行（如果有）、产物收集与释放走 31 Node。其他账号及其他 Skill 继续原路径；**不是全站、全 Skill 切换**。没有通过用户密码或伪造登录令牌代发消息，因此实际登录浏览器中的聊天请求需要用户亲自触发并观察结果。
- 31 上以假文本调用仓库 docx helper 成功生成 .docx，API 单元测试验证 Artifact(file_path=generated-artifacts/...) 读取远程校验后的 outputs，不会误读同名 155 宿主文件，也不会重复注册 Artifact。31 全仓、定向 race 与 vet 已通过；Node 与 AgentAPI 现均健康且没有残留任务容器。
- 风险：31 的公网 1Panel HTTP 42644 仍然开放，此前用户选择不关闭；本轮未动该服务。Node 控制面拥有 Docker 组权限。本次按用户新请求对其单一账号做真实灰度，**不能据此批准其他用户或其他 Skill 放量**。Office 的真实登录会话和生产对象存储下载尚待用户端到端验证；若失败，远程路径显式报错，不退回本地执行。
- 快速撤销此真实账号路由：在 155 执行 python3 /opt/agentapi/sandbox-canary-build/disable-real-user-route.py。它恢复仅假身份的 canary 用户配置并重建 AgentAPI，保留 DB revision 5 与节点服务，确认 /readyz=200。若需停用所有远程路线，再执行既有 SQL 策略回滚脚本（以当前活动 revision 为预期值）。


## 当前状态

- 31.25.10.10：开发节点服务 claude-codex-sandboxnode-dev.service 由独立 sandboxnode 用户运行，仅监听 127.0.0.1:18080。使用 digest 固定的 BusyBox 测试镜像；单槽开发池，network=none、非 root、只读 Skill/输入、64 MiB 工作区 tmpfs、256 MiB 内存、32 PIDs、0.5 CPU、1 MiB 产物上限。
- 155.94.154.240：AgentAPI 当前为现场可回滚的薄层镜像 claude-codex-agentapi:sandbox-canary-20261028，基础镜像为 3eaac5e；健康检查 /readyz 返回 200。数据库活动沙箱策略 revision 4，开发池端点为 https://node.mkason.com，最大并发 1。31 节点预期 revision 同为 4。
- 控制通道：31 上的 Caddy 仅把任务与开发执行路径对来源 155.94.154.240 代理到 Node 回环端口；非白名单来源返回 403，缺 HMAC 返回 401。155 的 claude-sandbox-reverse-tunnel.service 建立受限反向 SSH 隧道：31 本机 127.0.0.1:18081 指向 155 本机 127.0.0.1:8081。Node 缓存未命中时经该隧道按 digest 从 AgentAPI 私有制品接口下载；独立 Bearer 与 HMAC 密钥均为 mode-0600 文件，未写入 DB 或日志。
- API 灰度身份 sandbox-smoke-user 是不存在的测试身份，仅发布已有的 explain Skill。所有真实用户仍使用原执行路径，真实聊天请求没有切到 31。

## 真实服务器验收，不是本地 WSL

1. 在 31 上使用 Go 1.24.4：完整 go test -count=1 ./... 通过（131 个有测试结果的包）；指定模块的 go test -race 及 go vet 均通过。第一次全包失败是测试副本缺少 internal/ui 和项目 Office Skill 测试数据，补齐后重跑全绿。
2. 在 155 上用假数据通过 HTTPS 和 HMAC 完成 prepare、同租约两条命令、collect 和 dispose。Broker 从 DB revision 4 选择开发池，读取已发布的 explain Skill，调用真实 31 Node 并收到假产物。
3. 31 经私有 SSH 反向隧道访问 155 的 /readyz 为 200，制品 Bearer 下载为 200 且摘要相符；31 缓存出现 explain 的内容哈希。API 无 Bearer 返回 401，Node 缺签名返回 401，非 155 来源返回 403。
4. 假租户 Alice/Bob 的独立输入依次执行成功；跨用户执行及取消被拒，单槽阻止第二租户并发。容器内 Docker socket 与 Node 密钥不可见，输入和 Skill 只读，metadata 网络探针失败；超出 tmpfs 配额的写入被拒并清理。Docker 的沙箱任务标签下没有残留容器。
5. 真实 Node 重启前的已签名 nonce 首次触发预期 400，重启后同请求被拒为 401；nonce 和 journal 文件均为 mode-0600。过期清理、取消运行中命令和孤儿对账的故障注入在 31 的 Go 测试中通过。
6. AgentRuntime 单测证明远程产物通过 ArtifactService 登记并写入测试对象存储，非灰度用户保留旧执行器。生产对象存储和真实登录聊天会话尚未做远程端到端验收。

## 仍未解除的生产阻断项

- 用户之前明确选择不关闭 31 的公网明文 1Panel 管理端口 42644；本次未改动。Node 用户可访问 Docker 组，控制面权限很高。在完成该管理面风险及宿主机安全评审前，不能向 31 发送真实用户代码、外部 Skill、生产密钥或 Office 大任务。
- 当前 BusyBox 只适用于假数据验证，不含 Office/Python/Node 依赖。Office 输出、恶意后台进程、容器逃逸、资源峰值、完整多实例 fencing、长期 tombstone 压缩及断线后的持久交付队列仍需专项验证。journal 上限 4096，满时拒绝新任务，不会无限增长。
- 155 的 canary 镜像来自本次未提交代码的现场构建，尚不是 CI 发布的镜像。下次部署脚本会重置仓库到远端分支并清理未跟踪文件；推送代码后必须让 CI 构建新镜像并复测。勿将当前假数据 canary 当作真实用户放量许可。

## 回滚

1. 在 155 执行：python3 /opt/agentapi/sandbox-canary-build/rollback-policy.py。该脚本通过策略存储从原 revision 3 创建一个新的 disabled revision，保留审计；不要手工改活动 SQL 行。
2. 如需回退 AgentAPI，先参考 /opt/agentapi/sandbox-canary-build/config.before-20261028.env 与 docker-compose.before-20261028.yml 两份备份，再在 155 使用原 GHCR 3eaac5e 镜像重新创建 agentapi，确认 /readyz 为 200。
3. 31 的 Caddy 备份为 /etc/caddy/Caddyfile.pre-sandbox-20261028。回退时先验证 Caddy 配置再 reload，不要改变已有代理站点。测试输入位于单独命名的 sandbox-smoke 目录，未触碰用户工作区。

## 恢复与可用性补充

- 本轮共享本地工作区曾被外部操作重置到旧提交，新增沙箱源码一度消失；已从 31 节点私有测试目录中的同一份已验收源码恢复，并逐文件剔除了仅换行符差异。先前几个未跟踪的历史长篇计划文档无法逐字恢复，当前的 SANDBOX_REDESIGN_PLAN.md 和 CONFIGURATION.md 是明确标记的恢复摘要。本轮源码和文档另在 31 私有目录留有 mode-0600 压缩快照；推送前必须再次核对 Git 差异。
- SSH 反向制品隧道在长时间运行时曾出现一次超时重连，随后 /readyz 再次返回 200。缓存未命中的下载在隧道不可用时会拒绝，不能声称多节点/长时可用性已验收。生产流量仍禁止。
- SSH CI 工作流已恢复为固定 49222 端口并校验预先核对的 155 ED25519 主机指纹，以免下一次推送后 CI 退回旧的 22 端口或盲信扫描结果。
