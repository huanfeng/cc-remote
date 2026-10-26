import type { ReactNode } from 'react';
import { t } from './i18n';
import './tools.css';

type Fields = Record<string, unknown>;
const isFields = (value: unknown): value is Fields => value !== null && typeof value === 'object' && !Array.isArray(value);
const notReported = () => t('Not reported', '未报告');
const exact = (value: string) => <code style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{value}</code>;

/** Read-only details for RequestPermissions. Pass request.input, not the request envelope.
 * Approval controls stay with the caller; the bridge grants these permissions for one turn.
 */
export function CodexPermissionDetails({ input }: { input: unknown }) {
  let incomplete = false;
  const fields = (value: unknown, allowed: string[]): Fields => {
    if (!isFields(value)) { incomplete = true; return {}; }
    if (Object.keys(value).some(key => !allowed.includes(key))) incomplete = true;
    return value;
  };
  const source = isFields(input) ? input : {};
  if (!isFields(input)) incomplete = true;
  const row = (label: string, content: ReactNode) => <div className="question-item"><dt className="tool-detail-label">{label}</dt><dd style={{ margin: 0 }}>{content}</dd></div>;
  const text = (value: unknown) => {
    if (typeof value === 'string' && value.length) return exact(value);
    if (value != null && typeof value !== 'string') incomplete = true;
    return notReported();
  };
  const unavailable = () => { incomplete = true; return <span className="question-note">{t('Details unavailable or unrecognized', '详情不可用或无法识别')}</span>; };

  const pathView = (value: unknown): ReactNode => {
    if (!isFields(value)) return unavailable();
    if (value.type === 'path' || value.type === 'glob_pattern') {
      const glob = value.type === 'glob_pattern';
      const path = fields(value, ['type', glob ? 'pattern' : 'path']);
      const raw = path[glob ? 'pattern' : 'path'];
      return typeof raw === 'string' ? <>{glob ? t('Glob pattern', '通配符模式') : t('Path', '路径')}: {exact(raw)}</> : unavailable();
    }
    if (value.type !== 'special') return unavailable();
    fields(value, ['type', 'value']);
    if (!isFields(value.value)) return unavailable();
    const special = value.value;
    const labels: Record<string, string> = {
      root: t('Filesystem root', '文件系统根目录'), minimal: t('Minimal default directories', '最小默认目录集'),
      tmpdir: t('System temporary directory', '系统临时目录'), slash_tmp: t('/tmp directory', '/tmp 目录'),
      project_roots: t('Project roots', '项目根目录'), unknown: t('Unrecognized special directory', '未识别的特殊目录'),
    };
    const kind = special.kind;
    if (typeof kind !== 'string' || !Object.prototype.hasOwnProperty.call(labels, kind)) return unavailable();
    fields(special, ['kind', ...(kind === 'project_roots' || kind === 'unknown' ? ['subpath'] : []), ...(kind === 'unknown' ? ['path'] : [])]);
    const subpath = special.subpath == null ? null : <span> · {t('Subpath', '子路径')}: {text(special.subpath)}</span>;
    const unknownPath = kind === 'unknown' ? <span> · {t('Path', '路径')}: {typeof special.path === 'string' ? exact(special.path) : unavailable()}</span> : null;
    return <>{labels[kind]} ({exact(kind)}){unknownPath}{subpath}</>;
  };

  const paths = (value: unknown): ReactNode => {
    if (value == null) return notReported();
    if (!Array.isArray(value)) return unavailable();
    if (!value.length) return t('No additional paths listed', '未列出额外路径');
    return <ul>{value.map((path, index) => <li key={index}>{typeof path === 'string' ? exact(path) : unavailable()}</li>)}</ul>;
  };
  const entries = (value: unknown): ReactNode => {
    if (value == null) return notReported();
    if (!Array.isArray(value)) return unavailable();
    if (!value.length) return t('No additional entries listed', '未列出额外权限条目');
    return <ul>{value.map((entry, index) => {
      const item = fields(entry, ['access', 'path']);
      const access = item.access === 'read' ? t('Read', '读取') : item.access === 'write' ? t('Write', '写入') : item.access === 'deny' ? t('Deny', '拒绝') : undefined;
      if (!access) incomplete = true;
      return <li key={index}><strong>{access || t('Unknown access', '未知访问类型')}</strong>: {pathView(item.path)}</li>;
    })}</ul>;
  };

  const reason = text(source.reason), cwd = text(source.cwd);
  const permissions = fields(source.permissions, ['network', 'fileSystem']);
  if (!Object.keys(permissions).length) incomplete = true;
  const network = permissions.network == null ? {} : fields(permissions.network, ['enabled']);
  let networkText: ReactNode = notReported();
  if (network.enabled === true) networkText = t('Requested (enabled=true)', '请求网络访问（enabled=true）');
  else if (network.enabled === false) networkText = t('Not requested (enabled=false)', '未请求网络访问（enabled=false）');
  else if (network.enabled != null) networkText = unavailable();
  const fs = permissions.fileSystem == null ? {} : fields(permissions.fileSystem, ['read', 'write', 'entries', 'globScanMaxDepth']);
  const read = paths(fs.read), write = paths(fs.write), entryList = entries(fs.entries);
  const depth = fs.globScanMaxDepth == null ? null : row(t('Glob scan depth', '通配符扫描最大深度'),
    typeof fs.globScanMaxDepth === 'number' && Number.isSafeInteger(fs.globScanMaxDepth) && fs.globScanMaxDepth >= 1 ? fs.globScanMaxDepth : unavailable());

  return <section className="tool-detail" aria-label={t('Requested permissions', '请求的权限')}>
    <p className="question-note"><strong>{t('If approved, these permissions apply to this turn only.', '批准后，这些权限仅在本轮有效。')}</strong></p>
    <dl>
      {row(t('Reason', '申请原因'), reason)}
      {row(t('Working directory', '工作目录'), cwd)}
      {row(t('Network access', '网络访问'), networkText)}
      {row(t('Read paths', '读取路径'), read)}
      {row(t('Write paths', '写入路径'), write)}
      {row(t('Filesystem entries', '文件系统权限条目'), entryList)}
      {depth}
    </dl>
    {incomplete && <p className="question-note">{t('Some permission details are unavailable or unrecognized. Decline this request until the permissions can be verified.', '部分权限详情不可用或无法识别，请拒绝此请求，待权限范围可核实后再继续。')}</p>}
  </section>;
}
