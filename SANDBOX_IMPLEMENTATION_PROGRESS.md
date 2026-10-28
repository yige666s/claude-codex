
## 2026-10-28：真实节点开发灰度接线与服务器验收

详情及回滚见 SANDBOX_REAL_NODE_REPORT_2026-10-28.md。31 Node 与 155 API 已通过真实跨机的签名假数据闭环：DB revision 4、按 digest 经 SSH 私有隧道拉取 Skill 并缓存、Broker 多命令、产物、取消、nonce 重启防重放、Alice/Bob 假租户隔离及 tmpfs 配额。AgentAPI 健康，用户灰度名单仅含不存在的测试身份。没有真实登录用户的聊天请求或 Office Skill 端到端验收；31 公网 1Panel 风险依旧，因此禁止真实用户切流和 native。

补充：共享本地工作区曾被外部操作重置，已从 31 私有目录恢复当前源码并校验；历史未跟踪长篇文档只能以恢复摘要重建，不能假称逐字恢复。155→31 私有反向隧道出现过一次超时且自动重连；缓存未命中在隧道不可用时 fail-closed。推送前核查新增源码、CI 固定 49222、指纹及远端开发节点版本。


## 2026-10-28 单账号真实聊天灰度

活动策略已从 revision 4 升到 5；31 固定 digest 运行时镜像含 Python/Node/LibreOffice。155 的 AgentAPI 已以可回滚镜像部署，只有邮箱 yige29805@gmail.com 的一个 active 账号及 allowlist 中的 explain/docx 两个 Skill 走 31；其它账号/Skill 维持原路径。假 DOCX 在 31 完整生成并有界回传，Artifact(file_path) 改从 Node 已校验 outputs 读取，专项测试和 31 全仓 Go/race/vet 通过。**尚未由真实登录会话点击/发起并验证生产对象存储下载**；不能宣称所有 Skill 已切流。31 的公网明文 1Panel 风险未改，当前只是用户明确要求下的单账号灰度。快速回退脚本见 SANDBOX_REAL_NODE_REPORT_2026-10-28.md。
