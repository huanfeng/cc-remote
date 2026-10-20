# 独立网页前端

每台宿主都自带网页。部署了多台宿主时，也可以用一个单独部署的静态前端连接它们：侧栏宿主名称下的「N 个宿主」可切换、添加或移除宿主，并显示各宿主是否在线、有几个会话在执行或等待操作。

使用前需要满足三个条件：

1. 前端部署在一个 **HTTPS** 网站上（见[部署](#部署)）。
2. 每台宿主都通过 **HTTPS** 访问（见[必须使用 HTTPS](#必须使用-https)）。
3. 每台宿主都**允许**这个前端的来源（见[允许前端访问宿主](#允许前端访问宿主)）。

## 部署

| 方式 | 地址 | 适合 |
| --- | --- | --- |
| [公共实例（GitHub Pages）](#公共实例github-pages) | `https://huanfeng.github.io/cc-remote/` | 直接用，不想自己部署 |
| [Vercel](#vercel) | 你的 `*.vercel.app` 或自定义域名 | 想用自己的域名 |
| [Cloudflare Pages](#cloudflare-pages) | 你的 `*.pages.dev` 或自定义域名 | 想用自己的域名 |
| [任意静态网站](#任意静态网站) | 自定 | 已有 Nginx / Caddy 等 |

前端是纯静态文件，所有数据（宿主列表、令牌、草稿）只保存在你自己浏览器的本地存储里，不经过部署它的网站。可以放在子路径下。

### 公共实例（GitHub Pages）

每次发布正式版时，CI 会自动把前端发布到 <https://huanfeng.github.io/cc-remote/>。宿主上允许这个来源即可使用：

```sh
cc-remote service config --allow-origin https://huanfeng.github.io
```

需要了解的两点：

- **同源范围**：`huanfeng.github.io` 下的所有页面（该账号其它仓库的 Pages）与本前端同源，理论上可以读取它的本地存储，也就能读到保存的令牌；宿主允许这个来源后，这些页面也能调用宿主 API。介意的话请部署到自己的域名。
- **响应头**：GitHub Pages 不能自定义响应头，无法设置 `frame-ancestors`（防止被嵌入其它网页）。Vercel 和 Cloudflare Pages 可以，见下文。

在自己的 fork 上启用同样的自动发布：仓库 Settings → Pages → Source 选「GitHub Actions」；再到 Settings → Environments → `github-pages` → Deployment branches and tags，添加标签规则 `v*`（发版工作流在标签上运行，默认规则只允许默认分支）。之后每个正式版都会自动发布，也可以在 Actions 里手动运行「Pages」工作流。

### Vercel

1. 在 Vercel 导入这个仓库（或你的 fork），**Root Directory** 选 `web`。
2. 其它设置保持默认即可：`web/vercel.json` 已经写好安装命令、构建命令（`pnpm run build:standalone`）、输出目录（`dist-standalone`）和安全响应头。
3. 部署完成后，把得到的地址（如 `https://ccr-yourname.vercel.app`，或绑定的自定义域名）加入各宿主允许的来源（见[允许前端访问宿主](#允许前端访问宿主)）。

`vercel.json` 用 `corepack pnpm` 安装 `web/package.json` 里 `packageManager` 指定的 pnpm 版本，Node 版本请保持 22（项目设置 → Build and Deployment → Node.js Version）。

### Cloudflare Pages

**用 Git 构建**：Workers & Pages → Create → Pages → Connect to Git，选择仓库后：

| 设置 | 值 |
| --- | --- |
| Root directory | `web` |
| Build command | `corepack pnpm install --frozen-lockfile && corepack pnpm run build:standalone` |
| Build output directory | `dist-standalone` |
| 环境变量 | `NODE_VERSION=22`、`SKIP_DEPENDENCY_INSTALL=1`、`COREPACK_ENABLE_DOWNLOAD_PROMPT=0` |

`SKIP_DEPENDENCY_INSTALL=1` 让 Cloudflare 跳过它自己的依赖安装（它自带的 pnpm 版本可能与锁文件不符），改由构建命令安装。

**直接上传**：从 Release 下载 `cc-remote_<版本>_web.zip` 并解压，用 `npx wrangler pages deploy <解压目录> --project-name cc-remote` 上传，或在控制台拖入。

两种方式产物里都带有 `_headers` 文件，Cloudflare 会据此加上安全响应头。

### 任意静态网站

从 Release 下载 `cc-remote_<版本>_web.zip`，解压到 Nginx、Caddy 等静态网站即可，子路径也可以；也可以在源码中运行 `pnpm run build:standalone`（产物在 `web/dist-standalone`）。请只通过 HTTPS 提供，并加上响应头 `Content-Security-Policy: frame-ancestors 'none'` 和 `X-Content-Type-Options: nosniff`（页面自带的 CSP 无法设置 `frame-ancestors`）。

## 必须使用 HTTPS

浏览器不允许 HTTPS 页面访问 `http://` 的地址（混合内容），所以：

- **前端**必须通过 HTTPS 提供。前端通过明文 HTTP 打开时，页面会显示警告：它的本地存储里保存着所有宿主的令牌。
- **宿主**也必须通过 HTTPS 访问，例如放在 HTTPS 反向代理（Caddy、nginx）、Cloudflare Tunnel 后面，或由内网穿透工具提供 TLS。唯一的例外是本机地址 `localhost` / `127.0.0.1`。

CC Remote 本身只提供 HTTP，不处理证书。内网穿透出来的地址如果是 `http://公网IP:端口`，独立前端无法使用；需要换成「域名 + 证书」的形式，例如用 DDNS 域名加通配符证书得到 `https://home.example.com:45678`，端口变化也没关系。

宿主在局域网地址、前端在公网时，宿主会回应 Chrome 的私有网络访问预检；较新的浏览器还可能弹出「访问本地网络」的授权提示，允许即可。

## 允许前端访问宿主

来源只写协议、域名和端口，不带路径（GitHub Pages 的子路径 `/cc-remote/` 不要写）：

```sh
cc-remote service config --allow-origin https://ccr.example.com   # 后台服务：保存到 service.json，立即生效，无需重启
cc-remote --allow-origin https://ccr.example.com                  # 前台运行的宿主：启动参数
```

- `service config --allow-origin` **整体替换**允许的列表：有多个前端时用逗号一起列出（`--allow-origin https://a.example,https://b.example`），`--allow-origin ""` 清空。不带参数运行 `cc-remote service config` 可查看当前列表。
- 前端的登录页会显示这条命令（带本页的来源），点击即可复制。
- 环境变量 `CC_REMOTE_ALLOW_ORIGINS`（逗号分隔）仍然有效，与上面的设置合并；用 `service config --env` 修改它需要重启宿主才生效。
- 未列入的网站即使拿到令牌也无法在浏览器里调用 API（跨站请求返回 403，预检不放行）。宿主自带的网页不受此设置影响。

## 添加宿主

打开前端，选择连接方式，填写地址和该宿主的访问令牌：

| 连接方式 | 填写 | 适合 |
| --- | --- | --- |
| 直连 | 宿主地址，如 `https://ccr.example.com` | 地址固定 |
| 302 跳转 | 一个会跳转到宿主的地址 | 内网穿透工具提供的跳转链接、短链接 |
| 文本地址 | 一个以纯文本返回宿主地址的 URL | STUN 等端口会变化的穿透 |

后两种方式下，前端保存的是**入口地址**，并记住解析出来的宿主地址。宿主连不上时，前端会从入口重新获取地址；地址变了就自动切换过去并重新连接（同一宿主最多每分钟重试一次）。

### 302 跳转

前端请求入口地址，跟随跳转，取最终地址的协议、域名和端口作为宿主地址（路径会被忽略）。要求：

- **跳转服务**必须带 CORS 响应头 `Access-Control-Allow-Origin`（值为前端来源，或 `*`）。否则浏览器会把整个请求当作失败，前端连跳转去了哪里都看不到。
- **宿主**需要 v0.12.0 或更新版本，并且允许了前端的来源。浏览器跟随跨站跳转时，到达宿主那一跳的 `Origin` 是 `null` 而不是前端来源；允许了前端来源的宿主会对网页（非 API）路径的这类请求回应 CORS 头，跳转的终点因此可读。

### 文本地址

前端请求这个 URL，取响应正文的第一个词作为宿主地址，必须以 `https://` 开头（本机地址可以用 `http://`）。例如正文是：

```text
https://home.example.com:45678
```

这个 URL 同样必须带 `Access-Control-Allow-Origin` 响应头。可以用来提供它的服务：

- **GitHub 仓库或 Gist 的 raw 文件**：`raw.githubusercontent.com` 和 `gist.githubusercontent.com` 默认带 `Access-Control-Allow-Origin: *`。注意它们有几分钟的缓存，端口刚变时可能还拿到旧地址，稍后会再取。
- **Cloudflare Worker**：见下面的示例，可以由穿透工具在端口变化时调用接口更新。
- **自己的反向代理**：提供一个静态文件，并加上 CORS 响应头。

### 安全：入口决定令牌发往哪里

访问令牌会发给入口解析出的地址，所以入口服务和你的宿主同样重要：谁能改动入口，谁就能把前端引向自己的服务器，进而拿到令牌。

- 添加后，可以在「N 个宿主」列表里看到解析出的地址，请确认它确实是你的宿主。
- 之后自动切换**只接受端口变化**：协议或主机名变了，前端不会自动跟随，而是提示你重新添加该宿主来确认新地址。
- 入口请只用你自己控制的服务。公开的 Gist、raw 文件只适合放地址，不要让别人有改动权限。
- 解析入口时不会携带令牌和 Cookie。

### 示例：Cloudflare Worker

一个 Worker 同时提供文本地址（`GET /`）和跳转（`GET /go`），穿透工具在端口变化时用 `POST /` 更新。需要绑定一个 KV 命名空间 `ADDR`，并设置密钥 `UPDATE_TOKEN`：

```js
export default {
  async fetch(request, env) {
    const cors = { 'Access-Control-Allow-Origin': '*', 'Cache-Control': 'no-store' };
    const url = new URL(request.url);
    if (request.method === 'POST') {
      // 例：curl -X POST -H "Authorization: Bearer $UPDATE_TOKEN" -d https://home.example.com:45678 https://addr.example.workers.dev/
      if (request.headers.get('Authorization') !== `Bearer ${env.UPDATE_TOKEN}`) return new Response('forbidden', { status: 403 });
      const address = (await request.text()).trim();
      if (!/^https:\/\/[^\s/]+$/.test(address)) return new Response('bad address', { status: 400 });
      await env.ADDR.put('host', address);
      return new Response('ok');
    }
    const address = await env.ADDR.get('host');
    if (!address) return new Response('not set', { status: 404, headers: cors });
    if (url.pathname === '/go') return new Response(null, { status: 302, headers: { ...cors, Location: address + '/' } });
    return new Response(address, { headers: { ...cors, 'Content-Type': 'text/plain; charset=utf-8' } });
  },
};
```

前端里选「文本地址」填 `https://addr.example.workers.dev/`，或选「302 跳转」填 `https://addr.example.workers.dev/go`。`Access-Control-Allow-Origin` 也可以写成你的前端来源，不用 `*`。

## 编辑宿主

「N 个宿主」列表里每台宿主旁有编辑按钮，可以修改连接方式、地址（或入口）和令牌：

- 令牌留空表示保留原来的令牌。
- 保存前会用新的设置连接一次，连不上不会保存；地址或入口与列表里另一台宿主相同时也不会保存。
- 编辑的是当前宿主时，保存后页面重新加载并连到新地址；其它宿主只更新记录。
- 名称和颜色是宿主自己的设置（见[主机名称与颜色](configuration.md#主机名称与颜色)），每次连接时从宿主读取，这里不能修改。

## 注意事项

- 宿主列表和各宿主的令牌保存在这个前端所在浏览器的本地存储中；「移除」会一并删除该宿主的令牌、草稿和标签页记录。各宿主的会话、草稿互不混用。
- 代理层的认证（Basic Auth、SSO 等）与独立前端不兼容，见 [security.md](security.md#独立网页前端跨站访问)。
