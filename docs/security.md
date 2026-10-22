# 安全说明

## 访问令牌 = 在你电脑上执行代码的权限

CC Remote 让网页驱动本机的 Claude Code。任何持有访问令牌的人都可以：

- 读取所有会话内容；
- 以你的身份让 Claude Code 在宿主上读写文件、执行命令；
- 把会话切换到 `bypassPermissions` 等跳过审批的模式（网页上的二次确认只防误点，不是安全边界）。

因此请像对待 SSH 私钥一样对待令牌：

- 令牌保存在数据目录的 `access-token`（权限 0600），也可用 `CC_REMOTE_TOKEN` 指定（至少 16 字符）。数据目录应只允许你自己的账户访问。
- 浏览器把令牌保存在当前站点的 localStorage，不放进 URL；在共用设备上用完请「退出」。
- 怀疑泄露时删除 `access-token` 并重启宿主（`cc-remote service restart`）以生成新令牌。

## 网络暴露

- 默认只监听 `127.0.0.1:8787`，只有本机可以访问。
- Web/API 是**明文 HTTP**。通过 `lan:PORT` 或具体 IP 暴露到局域网时，令牌和对话内容在网络上以明文传输。宿主启动时会对每个非回环地址在日志中打印警告，`service install/config` 也会提示，网页从非本机地址经 http 打开时也会显示提示。
- 绑定所有网卡（`all:PORT`、`0.0.0.0`、`::`）必须显式加 `--allow-insecure-remote`。
- CC Remote 不做网络穿透，也不提供 TLS。远程访问的加密需要由你自己的 HTTPS 反向代理、隧道或私有网络（如 VPN / WireGuard 类组网）提供。

## 反向代理

API 会拒绝 `Origin` 与访问主机不一致的请求（提示「不允许跨站访问」）。代理需要把浏览器访问的主机名传给宿主：保留原始 `Host`，或通过 `X-Forwarded-Host` 传递（0.2.3 起支持）。

代理还要转发 WebSocket：网页终端视图和 `ccr` 都经 `/api/terminals/{id}/stream` 的 WebSocket 连接，代理不转发时终端视图一直停在「连接中」，并提示「WebSocket 没能建立」。

Caddy、Traefik 默认两者都满足；nginx 默认会把 `Host` 改成上游地址，也不转发 WebSocket 的 `Upgrade` 头，需要显式设置：

```nginx
map $http_upgrade $connection_upgrade {         # 放在 http 块
    default upgrade;
    ''      close;
}

location / {
    proxy_pass http://127.0.0.1:8787;
    proxy_set_header Host $host;                # 或 X-Forwarded-Host $host;
    proxy_http_version 1.1;                     # WebSocket（终端视图、ccr）
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    proxy_read_timeout 1d;                      # 终端空闲时连接不被代理断开
    client_max_body_size 25m;                   # 附件上传上限为 20 MB
}
```

其它反向代理或穿透工具（Lucky、frp、宝塔等）请确认已开启 WebSocket 支持。

经代理访问时，代理本身就是公网入口：请在代理层加一道认证（Basic Auth、SSO 等），不要只依赖访问令牌。

## 独立网页前端（跨站访问）

允许的来源（`service config --allow-origin`、`--allow-origin` 或 `CC_REMOTE_ALLOW_ORIGINS`）列出的网站可以在浏览器里调用 API：宿主对这些来源回应 CORS 预检，并允许它们读取响应。请求仍需携带访问令牌；令牌只保存在使用该前端的浏览器里。

- 只列入你自己控制的网站。列入的网站一旦被篡改，它的脚本就能读取在该站登录过的令牌。
- 来源按协议、域名、端口精确匹配，不支持通配符。
- 代理层的认证（Basic Auth、SSO 等）与独立前端不兼容：跨站请求的 `Authorization` 头已用于访问令牌，浏览器也不会带上代理的 Cookie 或 Basic 凭据。需要这类认证时，独立前端访问的宿主请改用 VPN 等网络层限制。
- 独立前端的页面自带 CSP（不加载外部脚本和图片，Markdown 里的远程图片不会被请求）。`frame-ancestors` 只能通过响应头设置，请在静态站上配置 `Content-Security-Policy: frame-ancestors 'none'` 和 `X-Content-Type-Options: nosniff`，并只通过 HTTPS 提供（Vercel、Cloudflare Pages 的配置已包含，GitHub Pages 无法设置）。页面自身也会检测是否被嵌入框架，被嵌入时不渲染界面。
- 公共实例 `https://huanfeng.github.io` 与该账号其它仓库的 Pages 同源；介意的话请部署到自己的域名，见 [standalone.md](standalone.md#公共实例github-pages)。
- 通过入口（文本地址）添加的宿主，令牌发往入口解析出的地址：能改动入口的人就能把令牌引向自己的服务器。自动切换只接受端口变化，主机名或协议变化需要重新添加宿主确认；入口请只用自己控制的服务，见 [standalone.md](standalone.md#安全入口决定令牌发往哪里)。

## 仅限本机的接口

- `POST /api/admin/shutdown`（`service stop` 使用）必须来自回环地址、携带令牌，且不得带 `Forwarded`、`X-Forwarded-For`、`X-Forwarded-Host`、`X-Real-IP` 等代理头，因此经本机反向代理转发的远程请求也会被拒绝。
- `service` 子命令、`ccr` 等本机工具只连接回环地址（`ccr --host` 另外允许 HTTPS 地址，拒绝其它明文地址），访问令牌不会发往其它主机。
- 发送令牌前，本机工具先让宿主证明它持有令牌（`GET /api/hello?nonce=…`，宿主回 HMAC-SHA256(令牌, nonce)，令牌本身不发送）：宿主停止期间，同一台机器上的其它用户占用端口也拿不到令牌。`/api/hello` 不需要令牌，能连到宿主的人可以据此认出这是 CC Remote，但得不到令牌。旧版宿主没有这个接口：只在它持有数据目录的锁（`host.lock`，即它正在运行）、且该数据目录还没运行过新版宿主（新版启动时写入 `host-proof` 标记）时才被信任，所以这个兼容只在升级后、旧宿主重启前的一段时间内有效。
- 审批 MCP 和提问 Hook 通过本机独立监听端口回到宿主，每轮 / 每次启动使用独立的短期凭证；终端会话的 Hook 令牌只对该会话自己的 Hook 接口有效，写在 0600 文件中，不进入环境变量。宿主启动 Claude Code 时去掉环境中的 `CC_REMOTE_TOKEN`。这只是纵深防御：Claude 运行的命令和宿主是同一个系统用户，仍能读到数据目录里的 `access-token`。
- `ccr` 在 Claude Code 尚未信任的目录新建会话前，先询问是否信任该目录（与 `claude` 的信任对话框相同）；网页新建终端会话需勾选「信任此目录」。

## 其它要点

- 上传的附件下载需要令牌；非图片一律以 `Content-Disposition: attachment` 返回并带 `X-Content-Type-Options: nosniff`，HTML / XML / SVG / 脚本类以 `application/octet-stream` 返回。
- 状态面板中的日志尾部会隐去访问令牌和 Bearer 凭证。
- 网页查看文件（`GET /api/sessions/{id}/file`）需要令牌，只开放会话工作目录、会话中出现过的目录和出现过的文件所在目录（根目录、一级目录和用户主目录除外），符号链接和 Windows 目录联接按打开后的实际路径判断，网络共享（UNC）路径一律不打开（会话工作目录本身所在的共享除外），预览的单个文件最多 20 MiB。下载更大的文件时，网页先用令牌申请一个下载链接（`POST /api/sessions/{id}/file/link`），得到 `/api/download/<票据>`：票据是 48 位十六进制随机数，5 分钟内有效，只对应这一个文件，每次请求都按上述范围重新检查路径；下载请求本身不带令牌（浏览器原生下载无法带 `Authorization` 头），以 `application/octet-stream` + `attachment` 返回。持有令牌本来就能让 Claude 读取任意文件，这个范围是为了让查看器只触及会话相关的文件，不是额外的安全边界。返回的类型只有图片、PDF 或 `application/octet-stream`，不会作为 HTML / SVG 在页面中执行。
- 网页终端视图连接终端会话的 PTY 流（`/api/terminals/{id}/stream`，WebSocket）。浏览器的 WebSocket 无法带 `Authorization` 头，网页先用令牌申请票据（`POST /api/terminals/{id}/ticket`），再以 `?ticket=<票据>` 连接：票据是 48 位十六进制随机数，只对应这一个会话，30 秒内有效且只能用一次，令牌本身不进入 URL。WebSocket 握手与其它 API 一样检查 `Origin`（同源、经反向代理的 `X-Forwarded-Host`，或 `--allow-origin` 允许的网页前端）。`ccr` 仍用令牌头连接。
- 服务环境变量保存在 `service.json`（0600）中；通过 `--env` 传入的密钥是明文保存的。默认只自动捕获代理变量，不会捕获任何密钥。
- 「写入 Claude 用户配置」只改写 `model`、`effortLevel`、`permissions.defaultMode` 三项，写入前备份。

## 建议

1. 只在本机或可信局域网使用；需要外网访问时放在 HTTPS 反向代理或隧道之后，并在代理层加额外认证。
2. 不要使用 `--allow-insecure-remote`，除非你清楚网络中的每一台设备。
3. 谨慎使用 `bypassPermissions` / `dontAsk` / `auto`，尤其是在可从网络访问的宿主上。
4. 定期更新（`cc-remote update`），Release 附带 `checksums.txt`，安装脚本会校验 sha256。
