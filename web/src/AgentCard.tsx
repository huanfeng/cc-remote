import { memo, useEffect } from 'react';
import Markdown from './Markdown';
import { AgentIcon, CheckIcon, ChevronDownIcon, CloseIcon, TerminalIcon } from './icons';
import { LongPre, ToolGroup } from './ToolGroup';
import { formatDuration, type ToolCall, type TranscriptItem } from './toolGroups';
import { t } from './i18n';
import './tools.css';

/** A subagent or other background task as the host folds Claude's task events. */
export type TaskInfo = { id: string; toolUseId?: string; description: string; subagentType?: string; taskType?: string; background: boolean; status: string; progress?: string; lastTool?: string; summary?: string; tokens?: number; toolUses?: number; durationMs?: number; startedAt: string; endedAt?: string };

/** What nested tool groups and agent cards need from the conversation. */
export type NestedProps = {
  openGroups: Record<number, boolean>; toolsExpanded: boolean; onToggleGroup: (key: number, open: boolean) => void;
  openCalls: number[]; onToggleCall: (seq: number, open: boolean) => void; onLoadFull: (call: ToolCall) => void;
  tasks: Map<string, TaskInfo>;
};

const STATUS: Record<string, string> = { running: t('Running', '运行中'), completed: t('Completed', '已完成'), failed: t('Failed', '失败'), killed: t('Stopped', '已停止'), stopped: t('Stopped', '已停止'), interrupted: t('Interrupted', '已中断'), error: t('Failed', '失败'), missing: t('No result recorded', '未记录结果'), ok: t('Completed', '已完成') };
/** Running shows a spinner, finished states a mark; idle agents keep the agent icon. */
function StatusIcon({ status, shell }: { status: string; shell?: boolean }) {
  if (status === 'running') return <span className="tool-spinner" />;
  if (status === 'completed') return <CheckIcon size={13} />;
  if (['failed', 'interrupted', 'killed', 'stopped', 'error'].includes(status)) return <CloseIcon size={12} />;
  return shell ? <TerminalIcon size={13} /> : <AgentIcon size={13} />;
}
/** Kind label of a task: shell commands run in the background; agents are foreground or background. */
const taskKind = (task: TaskInfo) => task.taskType === 'local_bash' ? t('Background command', '后台命令') : task.background ? t('Background', '后台') : t('Foreground', '前台');
const toolCount = (n: number) => t(n === 1 ? '1 tool use' : `${n} tool uses`, `${n} 次工具`);
const compact = (n: number) => n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n);

/** Status of an Agent call: its task when known (background agents finish after their call returns), else the call. */
function agentStatus(call: ToolCall, task?: TaskInfo): string {
  if (call.status === 'running') return 'running'; // no result yet in a live session
  if (task) return task.status === 'running' && call.status === 'error' ? 'failed' : task.status;
  return call.status === 'ok' ? 'completed' : call.status === 'error' ? 'failed' : call.status;
}

function agentMeta(call: ToolCall, task?: TaskInfo): string {
  const parts: string[] = [];
  if (task?.toolUses) parts.push(toolCount(task.toolUses));
  if (task?.tokens) parts.push(`${compact(task.tokens)} tokens`);
  const ms = task?.durationMs || call.durationMs;
  if (ms) parts.push(formatDuration(ms));
  return parts.join(' · ');
}

function NestedItems({ items, nested }: { items: TranscriptItem[]; nested: NestedProps }) {
  return <>{items.map(item => {
    if (item.type === 'agent') return <AgentCard key={item.key} call={item.call} items={item.items} nested={nested} />;
    if (item.type === 'tools' || item.type === 'special') {
      const calls = item.type === 'tools' ? item.calls : [item.call];
      return <ToolGroup key={item.key} groupKey={item.key} calls={calls} open={nested.openGroups[item.key] ?? nested.toolsExpanded} onToggle={nested.onToggleGroup} openCalls={nested.openCalls} onToggleCall={nested.onToggleCall} onLoadFull={nested.onLoadFull} />;
    }
    const event = item.event;
    if (event.kind === 'assistant' && event.text) return <div className="agent-message" key={item.key}><Markdown text={event.text} /></div>;
    return null;
  })}</>;
}

/**
 * A subagent run: header with type, description, status and live progress; opened it shows
 * the prompt, the subagent's own messages and tool calls, and its result.
 */
function AgentCardView({ call, items, nested }: { call: ToolCall; items: TranscriptItem[]; nested: NestedProps }) {
  const task = nested.tasks.get(call.id);
  const open = nested.openGroups[call.seq] ?? nested.toolsExpanded;
  const status = agentStatus(call, task);
  const running = status === 'running';
  const background = task?.background || call.input?.run_in_background === true;
  const type = task?.subagentType || call.input?.subagent_type || '';
  const placeholder = call.input?.placeholder === true || (call.seq < 0 && !call.input?.description);
  const description = task?.description || call.input?.description || (placeholder ? t('Subagent started earlier', '更早启动的子 agent') : t('Subagent', '子 agent'));
  const prompt = typeof call.input?.prompt === 'string' ? call.input.prompt : '';
  // The task's summary is the subagent's report itself: a background agent's call result only
  // says it launched, and a foreground one's wraps the report in harness notes.
  const result = task?.summary || (background ? '' : call.resultText);
  const meta = agentMeta(call, task);
  const cut = String(call.truncated || '');
  useEffect(() => { if (open && cut) nested.onLoadFull(call); }, [open, cut]);
  return <section className={`agent-card is-${status}${open ? ' open' : ''}`} data-agent={call.id}>
    <button type="button" className="agent-summary" aria-expanded={open} onClick={() => nested.onToggleGroup(call.seq, !open)}>
      <span className={`agent-icon is-${status}`}><StatusIcon status={status} /></span>
      <span className="agent-title"><strong>{description}</strong>{type && <span className="agent-type">{type}</span>}{background && <span className="agent-type">{t('Background', '后台')}</span>}</span>
      {meta && <span className="agent-meta">{meta}</span>}
      <span className={`tool-badge agent-status is-${status}`}>{STATUS[status] || status}</span>
      <span className="tool-group-chevron"><ChevronDownIcon size={14} /></span>
    </button>
    {running && task?.progress && <div className="tool-current">{t('Now: ', '正在：')}<span>{task.progress}</span></div>}
    {open && <div className="agent-body">
      {placeholder && !prompt && <p className="tool-detail-note">{t('This subagent was started in earlier messages; scroll up to load them and see its task.', '这个子 agent 的调用在更早的消息里，向上滚动加载后可以看到任务说明。')}</p>}
      {prompt && <><div className="tool-detail-label">{t('Task', '任务说明')}</div><LongPre text={prompt} /></>}
      {items.length > 0 && <div className="agent-transcript"><NestedItems items={items} nested={nested} /></div>}
      {items.length === 0 && !running && !result && <p className="tool-detail-note">{t('The subagent\'s steps were not recorded (terminal history and shared sessions keep only the final result)', '没有记录子 agent 的过程（终端历史和共享会话只保存最终结果）')}</p>}
      {running ? <p className="tool-detail-note">{t('Subagent running…', '子 agent 执行中…')}</p> : result ? <><div className="tool-detail-label">{status === 'failed' ? t('Result (failed)', '结果（失败）') : t('Result', '结果')}</div><div className="agent-result"><Markdown text={result} /></div></> : null}
    </div>}
  </section>;
}

export const AgentCard = memo(AgentCardView);

/** Panel of the session's subagents and background tasks; picking one jumps to its card. */
export function TaskPanel({ tasks, close, locate }: { tasks: TaskInfo[]; close: () => void; locate: (task: TaskInfo) => void }) {
  const ordered = [...tasks].sort((a, b) => Number(b.status === 'running') - Number(a.status === 'running') || Date.parse(b.startedAt) - Date.parse(a.startedAt));
  return <div className="modal-backdrop" onClick={close}><div className="modal task-panel" onClick={event => event.stopPropagation()}>
    <div className="modal-heading"><h2>{t('Subagents and background tasks', '子 agent 与后台任务')}</h2><button className="icon" aria-label={t('Close task panel', '关闭任务面板')} onClick={close}><CloseIcon size={18} /></button></div>
    {ordered.length === 0 ? <p>{t('This session has no subagents or background tasks yet.', '这个会话还没有子 agent 或后台任务。')}</p> : <ul className="task-list">{ordered.map(task => {
      const meta = [task.toolUses ? toolCount(task.toolUses) : '', task.tokens ? `${compact(task.tokens)} tokens` : '', task.durationMs ? formatDuration(task.durationMs) : ''].filter(Boolean).join(' · ');
      return <li key={task.id}><button type="button" className={`task-row is-${task.status}`} disabled={!task.toolUseId} onClick={() => locate(task)}>
        <span className="task-row-head"><span className={`agent-icon is-${task.status}`}><StatusIcon status={task.status} shell={task.taskType === 'local_bash'} /></span><strong>{task.description || task.id}</strong><span className={`tool-badge agent-status is-${task.status}`}>{STATUS[task.status] || task.status}</span></span>
        <small>{[task.subagentType, taskKind(task), meta].filter(Boolean).join(' · ')}</small>
        {task.status === 'running' && task.progress && <small className="task-progress">{t('Now: ', '正在：')}{task.progress}</small>}
      </button></li>;
    })}</ul>}
  </div></div>;
}
