# 打分参数排查 h1

## 结论 h2

### 细节 h3

#### 附注 h4

推导结果如下（单换行应当保留，
就像终端里那样）：

| 键 | 出厂值 | 含义 | 配万象模型时的推导值 |
|---|---|---|---|
| collocation_max_length | 4 | context 和 word 各取几个字去查搭配 | 6 |
| collocation_min_length | 2 | 搭配总长不到这个值（且不是整串匹配）就判为弱搭配 | 2 |
| weak_extra_penalty | 12.0 | 弱搭配额外扣的分 | 86 |
| rear_extra_penalty | 6.0 | 句末搭配额外扣的分 | 4 |
| baseline | 8.34 | 打分零点（取 ln 频次的中位数） | 8.34（通用） |

| 左对齐 | 居中 | 右对齐 | 路径 | 说明 |
|:---|:---:|---:|---|---|
| `a` | ✅ | 1 | `internal/server/very/long/path/to/some/deeply/nested/module/handler_test.go:1234` | 很宽的表格在手机上应横向滚动，而不是被压扁成一个字一行 |
| `bb` | ❌ | 22 | `web/src/Markdown.tsx:42` | ~~删除线~~ 与 **粗体** |

`★ Insight ─────────────────────────────────────`
- beam 宽度只影响候选保留数量，不改变打分函数本身
- 所以只调 beam 并没有解决…对齐的问题。
`─────────────────────────────────────────────────`

★ Insight ─────────────────────────────────────
第一行说明
第二行说明，引用 `src/a.ts:42`
没有解决…对齐的问题。 beam_backtrack 用 ... ─────────────────────────────────────────────────

普通段落，然后是一条裸分隔线：
─────────────────────────────────────────────────
分隔线之后的段落。

```text
┌──────────┬──────────────┐
│ 模块     │ 职责         │
├──────────┼──────────────┤
│ host     │ 托管会话     │
│ channel  │ 连接终端     │
└──────────┴──────────────┘
★ Insight ─────────────────  (代码块里不应被转换)
─────────────────────────────
```

```go
func veryLongFunctionNameThatOverflowsTheViewport(ctx context.Context, sessionID string, payload map[string]any, options ...Option) (result *Result, err error) {
	return nil, fmt.Errorf("this line is intentionally long so the block must scroll horizontally instead of overflowing the page: %w", err)
}
```

长路径：`C:\Users\me\AppData\Local\Temp\claude\D--Projects-example-workspace\00000000-0000-4000-8000-000000000000\scratchpad\md\cc-remote.exe`

长 URL：https://example.com/a/very/long/url/that/never/breaks/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa?query=1&other=2

无空格长串：aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

- 一级列表
  - 二级列表
    1. 三级有序
    2. 第二项
- [x] 已完成任务
- [ ] 待办任务

> 引用块：终端里的提示
> 第二行引用

链接：[文档](https://example.com/docs) 与危险链接 [click](javascript:alert(1))，脚注[^1]。

![图片](https://example.com/wide-image.png)

***

表情与符号：🎉 ★ ✓ ◇ → ⚠️

[^1]: 这是脚注内容。
