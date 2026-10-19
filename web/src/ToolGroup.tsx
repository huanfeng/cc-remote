import { memo, useEffect, useState, type ReactNode } from 'react';
import { AgentIcon, CheckIcon, ChevronDownIcon, CloseIcon, FileIcon, GlobeIcon, ListIcon, SearchIcon, TerminalIcon, WrenchIcon } from './icons';
import { argSummary, formatDuration, groupStats, summaryLabel, type CallStatus, type ToolCall } from './toolGroups';
import { useFiles } from './fileContext';
import { t } from './i18n';
import './tools.css';

const MAX_LINES = 60, MAX_CHARS = 6000;

/** Monospace block that shows the head of long output with a "show all" toggle. */
export function LongPre({ text, className = '' }: { text: string; className?: string }) {
  const [full, setFull] = useState(false);
  const lines = text.split('\n');
  const long = lines.length > MAX_LINES || text.length > MAX_CHARS;
  const shown = full || !long ? text : lines.slice(0, MAX_LINES).join('\n').slice(0, MAX_CHARS);
  return <div className={`tool-pre${className ? ' ' + className : ''}`}>
    <pre>{shown}{!full && long ? '\n…' : ''}</pre>
    {long && <button type="button" className="tool-more" onClick={() => setFull(!full)}>{full ? t('Show less', '收起') : t(`Show all (${lines.length} lines)`, `显示全部（共 ${lines.length} 行）`)}</button>}
  </div>;
}

export function toolIcon(name: string): ReactNode {
  if (/^(Bash|PowerShell|BashOutput|KillShell)$/.test(name)) return <TerminalIcon size={13} />;
  if (/^(Read|Write|Edit|MultiEdit|NotebookEdit|NotebookRead)$/.test(name)) return <FileIcon size={13} />;
  if (/^(Grep|Glob|WebSearch|ToolSearch)$/.test(name)) return <SearchIcon size={13} />;
  if (name === 'WebFetch') return <GlobeIcon size={13} />;
  if (name === 'Task' || name === 'Agent') return <AgentIcon size={13} />;
  if (name === 'TodoWrite') return <ListIcon size={13} />;
  return <WrenchIcon size={13} />;
}

function StatusMark({ status }: { status: CallStatus }) {
  if (status === 'ok') return <span className="tool-status is-ok" title={t('Succeeded', '成功')}><CheckIcon size={13} /></span>;
  if (status === 'error') return <span className="tool-status is-error" title={t('Failed', '失败')}><CloseIcon size={12} /></span>;
  if (status === 'running') return <span className="tool-status is-running" title={t('Running', '执行中')}><span className="tool-spinner" /></span>;
  return <span className="tool-status is-missing" title={t('No result recorded', '未记录结果')}>—</span>;
}

function inputText(input: any): string {
  if (input === undefined) return '';
  if (typeof input === 'string') return input;
  if (input && typeof input.command === 'string') return input.command + (input.description ? `\n\n# ${input.description}` : '');
  return JSON.stringify(input, null, 2);
}

type LoadFull = (call: ToolCall) => void;

/** Listings carry long tool text cut short; an opened call fetches its events whole. */
function ToolDetail({ call, onLoadFull }: { call: ToolCall; onLoadFull?: LoadFull }) {
  const input = inputText(call.input);
  const cut = String(call.truncated || '');
  const files = useFiles();
  const arg = argSummary(call.name, call.input);
  useEffect(() => { if (cut) onLoadFull?.(call); }, [cut]);
  return <div className="tool-detail">
    {files && arg.dir !== undefined && arg.title && <button type="button" className="tool-open-file" title={arg.title} onClick={() => files.open({ path: arg.title! })}><FileIcon size={13} />{t('Open file', '打开文件')}<span>{arg.title}</span></button>}
    {cut && <p className="tool-detail-note">{t('Long content, loading all of it…', '内容较长，正在加载完整内容…')} <button type="button" className="tool-more" onClick={() => onLoadFull?.(call)}>{t('Retry', '重试')}</button></p>}
    {input && <><div className="tool-detail-label">{t('Input', '输入')}</div><LongPre text={input} /></>}
    <div className="tool-detail-label">{call.status === 'error' ? t('Result (failed)', '结果（失败）') : t('Result', '结果')}</div>
    {call.status === 'running' ? <p className="tool-detail-note">{t('Running…', '执行中…')}</p> : call.status === 'missing' ? <p className="tool-detail-note">{t('No result recorded', '未记录结果')}</p> : <LongPre text={call.resultText || t('(no output)', '（无输出）')} className={call.status === 'error' ? 'is-error' : ''} />}
  </div>;
}

function CallArg({ call }: { call: ToolCall }) {
  const arg = argSummary(call.name, call.input);
  if (!arg.text) return <span className="tool-call-arg" />;
  return <span className="tool-call-arg" title={arg.title || arg.text}>{arg.dir !== undefined ? <><strong>{arg.text}</strong>{arg.dir && <span className="tool-call-dir">{arg.dir}</span>}</> : arg.text}</span>;
}

function ToolCallRow({ call, open, onToggle, onLoadFull }: { call: ToolCall; open: boolean; onToggle: (open: boolean) => void; onLoadFull?: LoadFull }) {
  return <li className={`tool-call is-${call.status}${open ? ' open' : ''}`} data-call={call.id || undefined}>
    <button type="button" className="tool-call-line" aria-expanded={open} onClick={() => onToggle(!open)}>
      <span className="tool-call-icon">{toolIcon(call.name)}</span>
      <span className="tool-call-name">{call.name}</span>
      <CallArg call={call} />
      {call.durationMs !== undefined && <span className="tool-call-time">{formatDuration(call.durationMs)}</span>}
      <StatusMark status={call.status} />
    </button>
    {open && <ToolDetail call={call} onLoadFull={onLoadFull} />}
  </li>;
}

/** One summary row for a run of tool calls; the call list only mounts while expanded. */
function ToolGroupView({ groupKey, calls, open, onToggle, openCalls, onToggleCall, onLoadFull }: { groupKey: number; calls: ToolCall[]; open: boolean; onToggle: (key: number, open: boolean) => void; openCalls: number[]; onToggleCall: (seq: number, open: boolean) => void; onLoadFull?: LoadFull }) {
  const stats = groupStats(calls);
  const label = summaryLabel(calls);
  return <section className={`tool-group${open ? ' open' : ''}${stats.running ? ' running' : ''}`}>
    <button type="button" className="tool-group-summary" aria-expanded={open} title={label} onClick={() => onToggle(groupKey, !open)}>
      <span className="tool-group-icon">{stats.running ? <span className="tool-spinner" /> : <WrenchIcon size={13} />}</span>
      <span className="tool-group-label">{label}</span>
      {stats.failed > 0 && <span className="tool-badge is-failed">{t(`${stats.failed} failed`, `${stats.failed} 个失败`)}</span>}
      {stats.running > 0 && <span className="tool-badge is-running">{t('Running', '执行中')}</span>}
      <span className="tool-group-chevron"><ChevronDownIcon size={14} /></span>
    </button>
    {!open && stats.current && <div className="tool-current">{t('Running: ', '正在执行：')}<strong>{stats.current.name}</strong> <span>{argSummary(stats.current.name, stats.current.input).text}</span></div>}
    {open && <ul className="tool-calls">{calls.map(call => <ToolCallRow key={call.seq} call={call} open={openCalls.includes(call.seq)} onToggle={value => onToggleCall(call.seq, value)} onLoadFull={onLoadFull} />)}</ul>}
  </section>;
}

export const ToolGroup = memo(ToolGroupView);
