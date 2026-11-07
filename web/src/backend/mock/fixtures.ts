// Mock data for the phase-1 frontend. Every shape matches what the host sends
// (see docs/redesign-inventory.md), so the same components later render real
// sessions unchanged. Each session exercises a different part of the UI.
import type {
  HostInfo, HostStatus, ModelList, NativeSession, PendingRequest, ProfileCatalog, Session, SessionEvent, TaskInfo,
  UsageReply,
} from '../types';

const NOW = Date.now();
export const ago = (minutes: number) => new Date(NOW - minutes * 60_000).toISOString();

/** Builds a transcript with increasing seq numbers and timestamps. */
class Script {
  events: SessionEvent[] = [];
  private seq = 0;
  private at: number;
  constructor(startMinutesAgo: number) { this.at = NOW - startMinutesAgo * 60_000; }
  private push(kind: string, fields: Partial<SessionEvent>, stepSeconds = 4) {
    this.at += stepSeconds * 1000;
    this.events.push({ seq: ++this.seq, kind, createdAt: new Date(this.at).toISOString(), ...fields });
    return this;
  }
  user(text: string, payload?: object) { return this.push('user', { text, payload }, 20); }
  assistant(text: string) { return this.push('assistant', { text }, 6); }
  tool(id: string, name: string, input: object, parent?: string) {
    return this.push('tool', { payload: { type: 'tool_use', id, name, input, ...(parent ? { parent_tool_use_id: parent } : {}) } }, 1);
  }
  result(id: string, content: string, opts: { error?: boolean; seconds?: number; parent?: string } = {}) {
    return this.push('tool_result', {
      payload: { tool_use_id: id, content, is_error: !!opts.error, ...(opts.parent ? { parent_tool_use_id: opts.parent } : {}) },
    }, opts.seconds ?? 2);
  }
  status(payload: object, text?: string) { return this.push('status', { payload, text }, 1); }
  compact(pre: number, post: number) {
    return this.push('compact', { payload: { compact_metadata: { trigger: 'auto', pre_tokens: pre, post_tokens: post } } }, 3);
  }
  recap(text: string) { return this.push('recap', { text }, 1); }
  error(text: string) { return this.push('error', { text }, 2); }
  teammate(text: string, name: string, color: string) { return this.push('teammate', { text, payload: { name, color } }, 5); }
  interaction(payload: object) { return this.push('interaction', { payload }, 2); }
}

export const hostInfo: HostInfo = {
  version: '0.25.1',
  cwd: 'D:/Develop/workspace',
  claudeAvailable: true,
  claudeVersion: '2.4.12',
  codexAvailable: true,
  codexVersion: '0.92.0',
  providers: ['claude', 'codex'],
  attachCommand: 'ccr attach',
  language: 'zh',
  name: 'Cartech 工作站',
  color: '#3b82f6',
  icon: 'laptop',
  features: ['sendId', 'mentions', 'codex', 'codexSteer', 'queueControls'],
};

// ---------- s1: Claude managed, running, tool groups + subagent + queue ----------
const s1 = new Script(3)
  .user('用量轮询每 60 秒一次太频繁了，能不能改成按页面可见性和活动情况自适应？顺便把 stale 状态的显示修一下。')
  .assistant('我先看一下现在 `useUsage` 的轮询实现和后端 `/api/usage` 的缓存策略。')
  .tool('t1', 'Grep', { pattern: 'useUsage|/api/usage', path: 'web/src' })
  .result('t1', 'web/src/Usage.tsx:41: export function useUsage(\nweb/src/Usage.tsx:58:   const r = await api(`/api/usage${q}`)\nweb/src/StatusPanel.tsx:22: import { useUsage } from \'./Usage\'')
  .tool('t2', 'Read', { file_path: 'D:/Develop/workspace/cc-remote/web/src/Usage.tsx', offset: 30, limit: 60 })
  .result('t2', '30\texport type Window = { percent: number; resetsAt?: string };\n31\t\n41\texport function useUsage(sessionId?: string, provider: Provider = \'claude\') {\n42\t  const [usage, setUsage] = useState<UsageReply>();\n43\t  useEffect(() => {\n44\t    let timer = 0;\n45\t    const tick = async () => { ... };\n46\t    timer = window.setInterval(tick, 60_000);', { seconds: 1 })
  .tool('t3', 'Read', { file_path: 'D:/Develop/workspace/cc-remote/internal/server/usage.go' })
  .result('t3', 'package server\n\n// usageCacheTTL bounds how often the subscription endpoint is called.\nconst usageCacheTTL = 45 * time.Second', { seconds: 1 })
  .assistant('后端已经有 45 秒缓存，所以前端 60 秒的固定间隔其实大部分命中缓存。真正的问题是**页面隐藏时仍在轮询**，以及失败后 stale 标记没有在下次成功时清除。我派一个子代理去核对所有调用方。')
  .tool('a1', 'Agent', { description: '核对 useUsage 的调用方', subagent_type: 'Explore', prompt: '列出 web/src 中所有使用 useUsage 的组件，说明各自传入的参数。' })
  .tool('a1t1', 'Grep', { pattern: 'useUsage\\(', path: 'web/src' }, 'a1')
  .result('a1t1', 'web/src/App.tsx:612\nweb/src/StatusPanel.tsx:48\nweb/src/Usage.tsx:201', { parent: 'a1' })
  .tool('a1t2', 'Read', { file_path: 'D:/Develop/workspace/cc-remote/web/src/StatusPanel.tsx' }, 'a1')
  .result('a1t2', '48\t  const usage = useUsage(undefined, tab);', { parent: 'a1' })
  .result('a1', '共 3 处调用：App 顶栏（传 sessionId）、StatusPanel（按 provider 标签）、UsageOrb（复用顶栏数据）。', { seconds: 38 })
  .tool('t4', 'Edit', {
    file_path: 'D:/Develop/workspace/cc-remote/web/src/Usage.tsx',
    old_string: '    timer = window.setInterval(tick, 60_000);',
    new_string: '    const schedule = () => { timer = window.setTimeout(async () => { await tick(); schedule(); }, document.hidden ? 300_000 : 60_000); };\n    schedule();',
  })
  .result('t4', 'The file D:/Develop/workspace/cc-remote/web/src/Usage.tsx has been updated successfully.')
  .tool('t5', 'Bash', { command: 'pnpm --dir web run typecheck', description: '类型检查' })
  .result('t5', "src/Usage.tsx(52,7): error TS2304: Cannot find name 'schedule'.", { error: true, seconds: 9 })
  .assistant('类型检查报了一个作用域错误，`schedule` 定义在了 effect 外面。我修正一下。')
  .tool('t6', 'Edit', { file_path: 'D:/Develop/workspace/cc-remote/web/src/Usage.tsx', old_string: 'const schedule', new_string: 'function schedule' })
  .result('t6', 'The file has been updated successfully.')
  .tool('t7', 'Bash', { command: 'pnpm --dir web run typecheck && node --test web/tests/usage.test.mjs', description: '类型检查并运行用量测试' });
// t7 is still running.

const s1Session: Session = {
  id: 's1', provider: 'claude', title: '用量轮询改为自适应', cwd: 'D:/Develop/workspace/cc-remote', status: 'running',
  mode: 'managed', online: true, createdAt: ago(42), updatedAt: ago(0), lastConversationAt: ago(1),
  permissionMode: 'acceptEdits', model: 'opus', effort: 'high', profileName: 'Anthropic 官方',
  queued: [{ id: 'q1', text: '改完之后顺便更新一下 docs/architecture.md 里关于轮询节奏的描述', editable: true, canSendNow: true, delivery: 'pending' }],
  live: {
    model: 'Opus 5.5', modelId: 'claude-opus-5-5', effort: 'high', costUSD: 1.82, durationMs: 1_840_000,
    contextTokens: 96_400, contextSize: 200_000, contextPercent: 48.2, activity: '正在运行 Bash：类型检查并运行用量测试', permissionMode: 'acceptEdits',
  },
  activeTasks: 0,
};

// ---------- s2: Claude terminal session, waiting on a shared question ----------
const questionInput = {
  questions: [
    {
      question: '新前端的样式方案选哪个？',
      header: '样式方案',
      multiSelect: false,
      options: [
        { label: 'Tailwind v4 + Radix', description: '与 Codex、ZCode 同栈，组件可直接借用', preview: '@import "tailwindcss";\n@theme { --color-brand: … }' },
        { label: 'CSS Modules', description: '零新依赖，但弹层要自己实现' },
        { label: 'shadcn/ui 全套', description: '起步最快，但默认长相偏模板化' },
      ],
    },
    {
      question: '需要哪些平台？',
      header: '平台',
      multiSelect: true,
      options: [{ label: 'Web' }, { label: '移动端' }, { label: '桌面端（Tauri）' }],
    },
  ],
};
const s2 = new Script(15)
  .user('/plan 前端重做')
  .assistant('在动手前有两个决策需要你确认。')
  .tool('ask1', 'AskUserQuestion', questionInput);

const s2Session: Session = {
  id: 's2', provider: 'claude', title: '前端重做方案讨论', cwd: 'D:/Develop/workspace/cc-remote', status: 'waiting',
  mode: 'terminal', online: true, attached: 1, createdAt: ago(15), updatedAt: ago(2), lastConversationAt: ago(2),
  permissionMode: 'plan', model: 'opus', effort: 'max',
  live: { model: 'Opus 5.5', effort: 'max', costUSD: 0.41, durationMs: 300_000, contextTokens: 31_000, contextSize: 200_000, contextPercent: 15.5, activity: '等待你的回答', suggestion: '' },
};

// ---------- s3: Codex managed, plan + diff + activity cards ----------
const s3 = new Script(120)
  .user('给 internal/store 加一个按 provider 统计会话数的方法，并补单测。')
  .status({ plan: [
    { step: '阅读 store.go 中会话的存储结构', status: 'completed' },
    { step: '实现 CountByProvider', status: 'completed' },
    { step: '补充单元测试', status: 'completed' },
    { step: '运行 go test ./internal/store/...', status: 'completed' },
  ], turnId: 'turn1' })
  .status({ subtype: 'codex_activity', category: 'reasoning', itemId: 'r1', turnId: 'turn1', content: '会话按 id 存在 map 中，provider 为空时视为 claude，需要和前端 providerOf 保持一致。', clipped: false })
  .status({ subtype: 'codex_activity', category: 'command', itemId: 'c1', turnId: 'turn1', content: '$ go test ./internal/store/...\nok  \tgithub.com/huanfeng/cc-remote/internal/store\t0.412s', clipped: false })
  .status({ turnId: 'turn1', diff: [
    'diff --git a/internal/store/store.go b/internal/store/store.go',
    '--- a/internal/store/store.go',
    '+++ b/internal/store/store.go',
    '@@ -212,6 +212,19 @@ func (s *Store) Sessions() []Session {',
    ' \treturn out',
    ' }',
    ' ',
    '+// CountByProvider reports how many sessions each provider owns; an empty',
    '+// provider is Claude, as in the web client.',
    '+func (s *Store) CountByProvider() map[string]int {',
    '+\ts.mu.RLock()',
    '+\tdefer s.mu.RUnlock()',
    '+\tcounts := map[string]int{}',
    '+\tfor _, sess := range s.sessions {',
    '+\t\tcounts[cmp.Or(sess.Provider, "claude")]++',
    '+\t}',
    '+\treturn counts',
    '+}',
  ].join('\n') })
  .assistant('已添加 `CountByProvider`，空 provider 计为 claude，与前端 `providerOf` 一致；新增的 `TestCountByProvider` 覆盖了混合与空值两种情况，`go test ./internal/store/...` 通过。')
  .status({ tokenUsage: { total: { totalTokens: 48210, inputTokens: 40100, cachedInputTokens: 28000, outputTokens: 8110 }, last: { totalTokens: 21400 }, modelContextWindow: 272000 } });

const s3Session: Session = {
  id: 's3', provider: 'codex', title: 'store 按 provider 计数', cwd: 'D:/Develop/workspace/cc-remote', status: 'idle',
  mode: 'managed', online: true, createdAt: ago(120), updatedAt: ago(95), lastConversationAt: ago(95),
  permissionMode: 'workspace-write', model: 'gpt-5.5-codex', effort: 'medium',
  live: { model: 'gpt-5.5-codex', effort: 'medium', costUSD: 0, durationMs: 0, contextTokens: 0, contextSize: 0, contextPercent: 0 },
};

// ---------- s4: Claude managed, waiting on an approval ----------
const s4 = new Script(8)
  .user('清理一下 dist 和 node_modules/.cache，重新构建一遍')
  .assistant('好的，先删除旧产物。')
  .tool('b1', 'Bash', { command: 'rm -rf web/dist web/node_modules/.cache', description: '删除构建产物和缓存' });

const s4Session: Session = {
  id: 's4', provider: 'claude', title: '清理并重新构建', cwd: 'D:/Develop/workspace/cc-remote', status: 'waiting',
  mode: 'managed', online: true, createdAt: ago(8), updatedAt: ago(1), lastConversationAt: ago(1),
  permissionMode: 'default', model: 'sonnet', effort: 'medium',
  live: { model: 'Sonnet 5.5', effort: 'medium', costUSD: 0.05, durationMs: 40_000, contextTokens: 12_000, contextSize: 200_000, contextPercent: 6, activity: '等待批准：Bash' },
};

// ---------- s5: Claude, idle, compacted, recap, plan approved earlier ----------
const s5 = new Script(60 * 26)
  .user('帮我梳理一下 cc-remote 终端模式的整体架构，写进 docs/terminal.md')
  .assistant('我会先读 `internal/terminal` 和 `TerminalView.tsx`，再整理文档。')
  .tool('r1', 'Read', { file_path: 'D:/Develop/workspace/cc-remote/internal/terminal/pty.go' })
  .result('r1', 'package terminal\n\n// PTY wraps a ConPTY on Windows and a pty(7) elsewhere.')
  .tool('r2', 'Read', { file_path: 'D:/Develop/workspace/cc-remote/internal/server/terminal.go' })
  .result('r2', '// stream relays PTY output to every attached web client…')
  .compact(182_000, 24_000)
  .tool('p1', 'ExitPlanMode', { plan: '## 文档结构\n\n1. 概览：托管会话与终端会话的区别\n2. PTY 层：ConPTY / pty(7)\n3. 流转发：ticket → WebSocket，尺寸协商（最后调整者胜出、`held`）\n4. `ccr` 本地接入与 `/resume` 接管\n5. 故障与重连：4001、指数退避\n\n写完后在 README 链接过去。' })
  .result('p1', 'User has approved your plan. You can now start coding.')
  .interaction({ id: 'req-p1', toolName: 'ExitPlanMode', outcome: 'web', decision: 'allow', mode: 'acceptEdits' })
  .tool('w1', 'Write', { file_path: 'D:/Develop/workspace/cc-remote/docs/terminal.md', content: '# 终端模式\n…' })
  .result('w1', 'File created successfully at: D:/Develop/workspace/cc-remote/docs/terminal.md')
  .assistant(
    '`docs/terminal.md` 已写好，主要内容：\n\n' +
    '| 层 | 职责 | 关键文件 |\n|---|---|---|\n' +
    '| PTY | 启动 Claude Code，Windows 用 ConPTY | `internal/terminal/pty.go` |\n' +
    '| 转发 | ticket 鉴权、广播输出、尺寸协商 | `internal/server/terminal.go` |\n' +
    '| 网页 | xterm 渲染、跟随/控制尺寸、手机按键栏 | `web/src/TerminalView.tsx` |\n\n' +
    '`★ Insight ─────────────────────────────────────`\n' +
    '- PTY 只有一个尺寸，所以"最后一个调整的前端胜出"，网页在被他端调整后转为跟随模式。\n' +
    '- 关闭码 4001 表示 Claude Code 已退出，此时不再自动重连，避免反复拉起进程。\n' +
    '`─────────────────────────────────────────────────`\n\n' +
    '```ts\nconst ws = new WebSocket(`${base}/api/terminals/${id}/stream?ticket=${ticket}`);\nws.binaryType = \'arraybuffer\';\n```',
  )
  .recap('你离开期间：终端架构文档已完成并提交到 docs/terminal.md，README 增加了链接。');

const s5Session: Session = {
  id: 's5', provider: 'claude', title: '整理终端模式架构文档', cwd: 'D:/Develop/workspace/cc-remote', status: 'completed',
  mode: 'terminal', online: false, createdAt: ago(60 * 26), updatedAt: ago(60 * 25), lastConversationAt: ago(60 * 25),
  permissionMode: 'acceptEdits', model: 'opus', effort: 'high',
  live: { model: 'Opus 5.5', effort: 'high', costUSD: 3.4, durationMs: 2_400_000, contextTokens: 24_000, contextSize: 200_000, contextPercent: 12 },
};

// ---------- s6: failed session in another project ----------
const s6 = new Script(60 * 50)
  .user('部署到预发环境')
  .tool('d1', 'Bash', { command: 'pnpm deploy:staging' })
  .result('d1', 'Error: VERCEL_TOKEN is not set', { error: true, seconds: 3 })
  .error('API Error: 529 Overloaded. 已重试 3 次后放弃。');

const s6Session: Session = {
  id: 's6', provider: 'claude', title: '部署预发环境', cwd: 'D:/Develop/workspace/porttool', status: 'failed',
  mode: 'managed', online: false, createdAt: ago(60 * 50), updatedAt: ago(60 * 49), lastConversationAt: ago(60 * 49),
  live: { costUSD: 0.02, durationMs: 12_000, contextTokens: 4000, contextSize: 200_000, contextPercent: 2 },
};

// ---------- s7: Codex terminal-less idle, short ----------
const s7 = new Script(60 * 24 * 3)
  .user('解释一下 hostlock 包是干什么的')
  .assistant('`internal/hostlock` 确保同一台机器上只有一个 cc-remote 宿主进程在运行：启动时在数据目录下获取文件锁，拿不到锁就把命令转发给已在运行的实例。');

const s7Session: Session = {
  id: 's7', provider: 'codex', title: '解释 hostlock', cwd: 'D:/Develop/workspace/cc-remote', status: 'idle',
  mode: 'managed', online: false, createdAt: ago(60 * 24 * 3), updatedAt: ago(60 * 24 * 3), lastConversationAt: ago(60 * 24 * 3),
};

export const sessions: Session[] = [s1Session, s2Session, s4Session, s3Session, s5Session, s6Session, s7Session];

export const transcripts: Record<string, SessionEvent[]> = {
  s1: s1.events, s2: s2.events, s3: s3.events, s4: s4.events, s5: s5.events, s6: s6.events, s7: s7.events,
};

export const requests: PendingRequest[] = [
  { id: 'req-b1', sessionId: 's4', kind: 'approval', toolName: 'Bash', status: 'pending', createdAt: ago(1),
    input: { command: 'rm -rf web/dist web/node_modules/.cache', description: '删除构建产物和缓存' } },
  { id: 'req-ask1', sessionId: 's2', kind: 'question', toolName: 'AskUserQuestion', status: 'pending', createdAt: ago(2), input: questionInput },
];

export const tasks: Record<string, TaskInfo[]> = {
  s1: [
    { id: 'task-a1', toolUseId: 'a1', description: '核对 useUsage 的调用方', subagentType: 'Explore', background: false, status: 'completed',
      summary: '共 3 处调用', tokens: 18_400, toolUses: 2, durationMs: 38_000, startedAt: ago(30), endedAt: ago(29) },
    { id: 'task-bg', description: 'pnpm --dir web run dev', taskType: 'local_bash', background: true, status: 'running',
      progress: 'VITE v6.1.0  ready in 412 ms', startedAt: ago(25) },
  ],
};

export const nativeClaude: NativeSession[] = [
  { provider: 'claude', id: 'n1', title: '排查 ConPTY 中文乱码', cwd: 'D:/Develop/workspace/cc-remote', updatedAt: ago(60 * 5), lastConversationAt: ago(60 * 5) },
  { provider: 'claude', id: 'n2', title: 'Porttool 端口扫描优化', cwd: 'D:/Develop/workspace/porttool', updatedAt: ago(60 * 30), lastConversationAt: ago(60 * 30) },
  { provider: 'claude', id: 'n3', title: '写周报', cwd: 'C:/Users/Cartech', updatedAt: ago(60 * 24 * 6), conversationTimeEstimated: true },
];

export const nativeCodex: NativeSession[] = [
  { provider: 'codex', id: 'nc1', title: '迁移 go 1.25', cwd: 'D:/Develop/workspace/cc-remote', updatedAt: ago(60 * 9) },
];

export const usage: Record<'claude' | 'codex', UsageReply> = {
  claude: {
    available: true, provider: 'claude', plan: 'Max 20x', account: 'cloudman@apk.name',
    usage: {
      fiveHour: { percent: 37, resetsAt: new Date(NOW + 2.4 * 3600_000).toISOString(), name: '5h' },
      weekly: { percent: 64, resetsAt: new Date(NOW + 3.2 * 86_400_000).toISOString(), name: '7d' },
      models: [{ name: 'Opus', percent: 71, resetsAt: new Date(NOW + 3.2 * 86_400_000).toISOString() }],
    },
    fetchedAt: ago(0),
  },
  codex: {
    available: true, provider: 'codex', plan: 'Pro',
    usage: { windows: [
      { name: '5h', percent: 12, windowMinutes: 300, resetsAt: new Date(NOW + 4 * 3600_000).toISOString() },
      { name: 'weekly', percent: 28, windowMinutes: 10080, resetsAt: new Date(NOW + 5 * 86_400_000).toISOString() },
    ] },
    fetchedAt: ago(0),
  },
};

export const models: Record<'claude' | 'codex', ModelList> = {
  claude: {
    fetchedAt: ago(0), restricted: false,
    models: [
      { id: 'opus', label: 'Opus', source: 'alias', description: '最强能力，适合复杂任务', isDefault: true },
      { id: 'sonnet', label: 'Sonnet', source: 'alias', description: '速度与能力均衡' },
      { id: 'haiku', label: 'Haiku', source: 'alias', description: '最快' },
      { id: 'claude-opus-5-5', label: 'Opus 5.5', source: 'api' },
      { id: 'claude-sonnet-5-5', label: 'Sonnet 5.5', source: 'api' },
      { id: 'claude-fable-5-1', label: 'Fable 5.1', source: 'recent' },
    ],
  },
  codex: {
    fetchedAt: ago(0), restricted: false,
    models: [
      { id: 'gpt-5.5-codex', label: 'GPT-5.5 Codex', source: 'codex', efforts: ['low', 'medium', 'high', 'xhigh'], defaultEffort: 'medium', isDefault: true },
      { id: 'gpt-5.5', label: 'GPT-5.5', source: 'codex', efforts: ['minimal', 'low', 'medium', 'high'] },
    ],
  },
};

export const profiles: ProfileCatalog = {
  schemaVersion: 1,
  capabilities: { resumeSelection: true, switchSupplier: true, oauthProfiles: true, defaultProfile: true },
  source: 'cc-switch', found: true,
  profiles: [
    { id: 'p-official', name: 'Anthropic 官方', app: 'claude' },
    { id: 'p-relay', name: '中转 A', app: 'claude' },
    { id: 'oauth:yjw', name: 'yjw（OAuth）', app: 'claude', kind: 'oauth' },
    { id: 'p-codex', name: 'OpenAI 官方', app: 'codex' },
  ],
  defaultProfiles: { claude: 'p-official', codex: 'p-codex' },
  configuredDefaults: { claude: '' },
};

export const hostStatus: HostStatus = {
  version: '0.25.1', pid: 18244, startedAt: ago(60 * 7), uptimeSeconds: 7 * 3600, listen: '0.0.0.0:8787',
  listeners: [{ address: '127.0.0.1:8787', loopback: true }, { address: '192.168.1.20:8787', loopback: false, insecure: true }],
  dataDir: 'C:/Users/Cartech/AppData/Roaming/cc-remote', goVersion: 'go1.25.3', os: 'windows', arch: 'amd64',
  claude: { executable: 'claude', path: 'C:/Users/Cartech/.local/bin/claude.exe', version: '2.4.12' },
  service: { managed: true, manager: 'Windows 计划任务' },
  sessions: { total: 7, managed: 5, terminal: 2, running: 1, waiting: 2, idle: 4 },
  pendingRequests: 2, activeRuns: 1,
  totals: {
    sessionId: '', turns: 412, results: 409, costUSD: 187.4, costKnown: true, inputTokens: 6_100_000, outputTokens: 1_900_000,
    cacheReadTokens: 38_400_000, cacheCreationTokens: 1_800_000, durationMs: 86_400_000, toolCalls: 6210, errors: 31, lastActivityAt: ago(2),
  },
  daily: Array.from({ length: 14 }, (_, i) => ({
    date: new Date(NOW - (13 - i) * 86_400_000).toISOString().slice(0, 10),
    turns: [12, 30, 22, 8, 0, 41, 37, 26, 19, 44, 52, 33, 28, 17][i],
    costUSD: [4, 11, 8, 2, 0, 16, 14, 9, 7, 18, 22, 12, 10, 6][i],
    tokens: 0,
  })),
  logTail: [
    '23:41:02 INFO session s1 turn started model=opus',
    '23:41:05 INFO usage refreshed provider=claude five_hour=37%',
    '23:42:17 WARN terminal s2 resize ignored: held by ccr',
  ],
};

/** Saved hosts shown by the switcher in mock mode (the real list is client-side, phase 2). */
export const demoHosts = [
  { id: 'h1', name: 'Cartech 工作站', url: 'https://cartech.lan:8787', color: '#3b82f6', ping: { online: true, running: 1, waiting: 2 } },
  { id: 'h2', name: 'MacBook Pro', url: 'https://mbp.tail1234.ts.net', color: '#10b981', ping: { online: true, running: 0, waiting: 0 } },
  { id: 'h3', name: '实验室服务器', url: 'https://lab.example.com', color: '#f59e0b', ping: { online: false, running: 0, waiting: 0 } },
];
