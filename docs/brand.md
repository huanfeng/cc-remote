# 图标与品牌资源

应用图标使用两个独立的大写 **C**，对应 CC Remote 的品牌缩写。两个字母等高、等粗，笔画之间保留清楚的间隔，不交叠或相连；保持深绿底色与浅绿图形。

- 主图源：[web/public/icon.svg](../web/public/icon.svg)，192 × 192 的矢量画布。
- 底色 `#222c27`，图形 `#d6ebbc`，12 单位圆头描边。字母绘制为矢量轮廓，不依赖外部字体、滤镜或图片引用，避免不同系统使用替代字体。
- 网页标签、PWA、README 和宣传页复用主图；桌面和托盘使用同一图形生成的原生格式。
- 宿主默认图标为 `logo`，保留宿主自己的底色。显式选择的 `code`、设备图标、文字和 emoji 继续使用原配置。

## 更新资源

修改主图后，同步 `web/public/host-icons.json` 中的 `logo` 图形与 `internal/server/identity.go` 的 `logoGlyph` 回退图形。前端宿主标识直接复用同一预设。

在仓库根目录，用已安装的桌面工具链生成各平台格式：

```sh
pnpm --dir desktop tauri icon ../web/public/icon.svg --output ../.test-data/generated-icons
```

将生成目录中的 `32x32.png`、`64x64.png`、`128x128.png`、`128x128@2x.png`、`icon.png`、`icon.ico` 和 `icon.icns` 回填到 `desktop/src-tauri/icons/`。其他移动平台资源无需加入当前仓库。

图标变化时递增 `web/public/sw.js` 中的资源缓存版本，避免已安装的网页继续读取旧图。系统或已安装的 PWA 也可能缓存应用图标；资源更新不代表已安装程序立刻替换系统快捷方式图标。

检查浅色和深色背景下的 16、24、32 像素效果，再构建网页与桌面资源。宣传页构建会自动复制更新后的主图。
