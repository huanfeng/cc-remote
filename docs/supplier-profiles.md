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

## 默认供应商

新建会话时没指定供应商，用的就是「默认供应商」，网页的新建会话和 `ccr` 完全一致。优先级：明确指定（网页下拉、`--profile`）> 你设置的默认 > CC-Switch 当前选中的供应商 > 宿主原生配置。

- 设置：网页「设置 → 会话默认 → 新会话供应商」，或命令行：

  ```sh
  cc-remote profile ls                 # 列出供应商，* 标记当前默认
  cc-remote profile default            # 查看默认
  cc-remote profile default 名称       # 指定默认（名称或 id，包括 OAuth 令牌配置）
  cc-remote profile default @native    # 默认用宿主原生配置
  cc-remote profile default follow     # 不单独指定，跟随 CC-Switch 当前项
  ```

  Codex 加 `--app codex`。设置保存在宿主数据目录的 `default-profiles.json`。
- 只影响之后新建的会话；已有会话保持创建时的供应商，恢复时仍可「沿用原配置」。
- 指定的默认供应商被删除或不可用时，新建会话会明确报错，提示重新选择，**不会悄悄换成别的供应商**。
- `cc-remote claude` 交互选择时，回车同样采用默认供应商。

## OAuth 令牌配置

CC-Switch 的供应商是「整体替换」：端点、密钥、模型变量都换成供应商的。OAuth 令牌配置是另一种东西，它**叠加在宿主原生配置之上**：会话照常使用宿主自己的 Claude Code 配置（模型、插件、hooks、MCP、权限），只是把登录令牌换成你保存的 `CLAUDE_CODE_OAUTH_TOKEN`。适合给不同的官方账号分别建一项。

- 生成令牌：在能登录的机器上运行 `claude setup-token`，得到 `sk-ant-oat…`。
- 添加：网页「设置 → OAuth 令牌」。令牌只保存在宿主数据目录的 `oauth-profiles.json`（权限 0600），添加后不会再显示，也不出现在任何列表接口里；请保护该目录。可更换令牌或删除。
- 选择：与 CC-Switch 供应商出现在同一个选择器里，名称后带「· OAuth」；`ccr` 的供应商列表同样带标记，`--profile oauth:<ID>` 可直接指定。
- 冲突处理：启动时清空 `ANTHROPIC_AUTH_TOKEN`、`ANTHROPIC_API_KEY`、`ANTHROPIC_BASE_URL`（它们优先于 OAuth 令牌，或会把令牌发到别的端点），其余 `ANTHROPIC_*`（包括模型变量）保持原生设置。
- 会话只记引用（`oauth:<ID>`），**每次启动现取令牌**：更换令牌后，使用它的会话下次启动即生效；条目被删除后，这些会话启动时会提示重新选择供应商。
- 命令行：`cc-remote oauth ls | add 名称 | token 名称 | rename 名称 新名称 | rm 名称`（名称也可写 `ls` 显示的 id）。`add` 和 `token` 的令牌从环境变量 `CC_REMOTE_OAUTH_TOKEN`、标准输入或隐藏输入读取，也可用 `--token`（会留在 shell 历史里，不推荐）。命令经由运行中的宿主完成，宿主没运行时会先启动。
- 接口：`POST /api/profiles/oauth`、`PATCH|DELETE /api/profiles/oauth/{ID}`，都需要访问令牌。

- 订阅用量：使用 OAuth 令牌的会话，顶栏额度和用量详情显示**该令牌所属账号**的 5 小时 / 每周额度（与官方登录一样），不再是宿主自己的登录。状态页「用量」处可切换查看宿主登录或任一 OAuth 账号；每个账号有独立的用量历史（宿主每 10 分钟记录一次，删除令牌时一并清除）。令牌失效时会提示更换。

限制：模型列表按宿主配置生成，不随供应商变化。

## 数据与兼容性

每次读取列表和明确选择供应商时重新查询 CC-Switch。会话保存私有配置快照，之后 CC-Switch 默认供应商变化不影响已有会话。API 只返回名称、ID、应用和不可用原因，不返回密钥；快照存于宿主私有数据库，请保护该数据目录。

读取仅依赖 providers 的 id、app_type、name、settings_config，允许额外列和版本变化。可选读取 meta、is_current、sort_index、公共配置和同目录 settings.json 的当前供应商标记。cc-remote 不写入 CC-Switch 数据库，也不修改其全局选中项。

Claude 读取 API 连接环境变量，按 commonConfigEnabled 合并公共配置；不导入权限、hooks 或 MCP。Codex 读取 auth.OPENAI_API_KEY 与 config TOML 的模型和连接字段，支持普通/带引号的表名、标量与独立公共配置。连接字段中的内联表、数组、多行字符串等未支持语法会列为不可用，不猜测解析。官方 OAuth 登录请选择“宿主原生配置”，使用宿主已有登录。

数据库缺失时可使用原生配置；损坏或必要字段缺失时显示明确错误。单个供应商不可用会显示原因，不阻止其它有效供应商使用。

## Windows 与 Linux

列表来自运行 cc-remote **宿主进程**的机器，而非浏览器所在机器。Windows 上的 CC-Switch 不会自动出现在 Linux 宿主上；可将其一致性数据库备份放到 Linux 并指定路径。复制前退出 CC-Switch，或使用 SQLite 备份工具，避免遗漏 WAL 中的更新。数据库包含密钥，请通过可信通道传输并限制访问权限。

Linux 无需运行 CC-Switch GUI。此功能只读取配置、启动和恢复会话，不做跨机数据库同步。
