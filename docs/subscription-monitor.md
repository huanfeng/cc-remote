# 订阅用量监控

监控属于 cc-remote 后台能力。后台启动后采集本机官方 Claude 登录、现有多个 Claude OAuth 授权，以及本机官方 Codex 登录；每 10 或 30 分钟记录一次。托盘和网页均只消费缓存。后台需要持续运行；关闭页面、Tauri 客户端或托盘不会停止采样。

状态页「订阅用量监控」可选账号、切换间隔、手动刷新和查看 24 小时 / 7 天 / 30 天历史。曲线记录订阅额度百分比及重置时间，不是精确 token 数、费用或逐请求账单。失败记为缺口，休眠期间不补造数据。手动刷新有一分钟冷却，并遵守各提供方现有缓存与退避。

## 账号与授权

- Claude 官方登录继续读取后台的 Claude 配置目录。多个 OAuth 授权直接使用 cc-remote 已有的 `oauth-profiles.json`，无需给托盘另配账号。
- Claude 使用官方 profile 接口确认 account UUID + organization UUID；Codex 使用 CLI app-server 返回的 user ID 和 workspace account ID。持久化的历史键为哈希，不使用邮箱、显示名称或 profile 名作为账号依据。
- 同一已确认账号重新登录、轮换 token、删除后重新添加授权，均延续其历史。切换到不同账号或组织会单独记录。删除授权停止后续采集，保留此前历史。
- 某些 `claude setup-token` 缺少 profile scope，无法确认真实身份。这类 token 独立记录；同一个 token 改名仍连续，新 token 不能安全自动归并。后续成功确认身份时，自动迁移该凭证自己的隔离记录。
- 旧版 Codex CLI 如果不提供稳定账号 ID，只展示当前额度，不积累可能混用账号的历史；升级 CLI 后启用按账号记录。Codex 本次支持本机当前官方登录，尚不包含多个独立 Codex OAuth profile。
- 延用 cc-remote 现有 OAuth 授权添加流程，本次没有新增浏览器 OAuth 登录向导，也不主动刷新 Claude token；过期登录由官方客户端更新后继续读取，避免干扰正在运行的登录。

Claude 额度读取延用现有机制：先调用官方 usage 接口；403/429 时可能发起一次 **最大输出 1 token 的推理请求**，读取额度响应头，可能消耗少量额度。setup-token 可能需要此回退。Codex 只调用 `account/read` 和 `account/rateLimits/read`，不启动推理回合。

## 数据与兼容

监控设置、缓存摘要和 token 指纹保存在后台数据目录 `usage-monitor.json`；原始 token 仍由既有凭证配置管理，不写入监控响应、命令行或历史。标准化采样存入原 SQLite 数据库的 `account_usage_samples`，来源为 `monitor:<account-id>`，保留 30 天。

升级前的记录没有可信的账号身份，继续保留在原数据表，可从状态页「查看旧版记录」访问，不猜测归并到新账号。旧后台不支持监控接口时，前端回退到原用量视图。

所有接口均要求后台访问令牌：

| 接口 | 行为 |
| --- | --- |
| `GET /api/usage/monitor` | 只读缓存与设置，不请求提供方 |
| `POST /api/usage/monitor/refresh` | 尝试刷新全部有效来源并记录 |
| `PUT /api/usage/monitor/settings` | `{ "intervalMinutes": 10 }` 或 `30`，持久化 |
| `GET /api/usage/monitor/history?account=…&hours=24` | 当前账号的历史，最长 720 小时 |

托盘开发和运行见 [tray/README.md](../tray/README.md)；完整历史客户端见 [desktop.md](desktop.md)。
