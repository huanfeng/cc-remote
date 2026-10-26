# CC-Switch 供应商选择

cc-remote 直接只读读取**宿主机器上**的 CC-Switch SQLite 数据库。不再使用 cc-tool，也不需要先导出供应商 JSON。

默认路径：Windows `%USERPROFILE%\.cc-switch\cc-switch.db`，Linux `~/.cc-switch/cc-switch.db`。

前台启动可指定数据库：

```powershell
cc-remote --cc-switch-db "D:\Config\cc-switch.db"
```

也可使用环境变量 `CC_REMOTE_CC_SWITCH_DB`。后台服务使用已有环境配置入口：

```powershell
cc-remote service config --env "CC_REMOTE_CC_SWITCH_DB=D:\Config\cc-switch.db"
```

## 使用

- Web 新建会话：选择 Claude Code 或 Codex，再选择对应供应商。默认采用 CC-Switch 当前供应商；也可明确选择“宿主原生配置”。
- Web 原生历史：选择会话，点击以终端继续或导入托管会话，在确认窗口选择供应商。默认沿用原配置；已导入的会话保持原供应商快照。
- `ccr --new`：交互终端会列出 Claude 供应商；`ccr -r` / `ccr -c` 在恢复时同样可以选择，回车沿用原配置。
- `ccr --new --profile <供应商ID>` 或 `ccr -r <会话ID> --profile <供应商ID>`：指定供应商。支持数据库原 ID 或列表中的 `ccswitch:` ID。
- `--profile @native`：明确使用宿主原生配置。非交互恢复且没有指定 `--profile` 时保持原会话配置。
- 已运行会话：设置中切换供应商；忙碌时需要确认中断，保留原会话 ID 和历史，不自动重发请求。
- 会话标题下方持续显示所选供应商，新建后尚未发送消息也会显示；名称来自会话保存的配置快照，刷新后保留。

## 数据与兼容性

每次读取列表和明确选择供应商时重新查询 CC-Switch。会话保存私有配置快照，之后 CC-Switch 默认供应商变化不影响已有会话。API 只返回名称、ID、应用和不可用原因，不返回密钥；快照存于宿主私有数据库，请保护该数据目录。

读取仅依赖 providers 的 id、app_type、name、settings_config，允许额外列和版本变化。可选读取 meta、is_current、sort_index、公共配置和同目录 settings.json 的当前供应商标记。cc-remote 不写入 CC-Switch 数据库，也不修改其全局选中项。

Claude 读取 API 连接环境变量，按 commonConfigEnabled 合并公共配置；不导入权限、hooks 或 MCP。Codex 读取 auth.OPENAI_API_KEY 与 config TOML 的模型和连接字段，支持普通/带引号的表名、标量与独立公共配置。连接字段中的内联表、数组、多行字符串等未支持语法会列为不可用，不猜测解析。官方 OAuth 登录请选择“宿主原生配置”，使用宿主已有登录。

数据库缺失时可使用原生配置；损坏或必要字段缺失时显示明确错误。单个供应商不可用会显示原因，不阻止其它有效供应商使用。

## Windows 与 Linux

列表来自运行 cc-remote **宿主进程**的机器，而非浏览器所在机器。Windows 上的 CC-Switch 不会自动出现在 Linux 宿主上；可将其一致性数据库备份放到 Linux 并指定路径。复制前退出 CC-Switch，或使用 SQLite 备份工具，避免遗漏 WAL 中的更新。数据库包含密钥，请通过可信通道传输并限制访问权限。

Linux 无需运行 CC-Switch GUI。此功能只读取配置、启动和恢复会话，不做跨机数据库同步。
