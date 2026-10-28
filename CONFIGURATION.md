# 配置说明（2026-10-28）

非敏感部署引导项放在 deploy/local/config.env，敏感凭据放在 deploy/local/secrets.env 或只读私有密钥文件，绝不提交真实值。Compose 示例在 deploy/local/config.env.example 与 secrets.env.example。运行时沙箱 profile/route/node pool 使用 SQL 活动 revision 管理，Admin 的沙箱策略页面负责发布、审计和回滚；数据库只保存 credential_ref，不保存密钥内容。

远程 Skill 灰度引导项：AGENT_API_SANDBOX_CANARY_USER_IDS、AGENT_API_SANDBOX_SKILL_IDS、AGENT_API_SANDBOX_NODE_CREDENTIAL_REF。空的 canary 用户列表禁用远程接线；目前 155 使用不存在的测试身份。Skill 制品保存于 AgentAPI 持久卷内，HMAC 和制品下载密钥分别为 /run/agentapi/secrets/sandbox-node.key 与 sandbox-artifact.key（宿主文件必须仅 agentapi 可读）。Node 的受限开发配置文件为 /etc/claude-sandbox-node/config.json；不得在代码中写死 endpoint、密钥、image 或 origin 可信判定。

发布/回滚与当前剩余阻断见 SANDBOX_REAL_NODE_REPORT_2026-10-28.md；不要把活动 DB policy enabled 当作允许所有用户切流。部署脚本重置服务器 Git 工作区，下次推送前务必把当前本地修改和新增源码加入版本控制。
