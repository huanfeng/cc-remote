import { lang, setLang, t, type Lang } from './i18n';

const LANGS: { value: Lang; label: string }[] = [{ value: 'zh', label: '中文' }, { value: 'en', label: 'EN' }];

/** A small 中文 / EN switch for the pages shown before signing in; settings has the full choice. */
export function LangSwitch() {
  return <div className="lang-switch" role="group" aria-label={t('Interface language', '界面语言')}>
    {LANGS.map(x => <button type="button" key={x.value} aria-pressed={lang === x.value} className={lang === x.value ? 'active' : ''} onClick={() => { if (lang !== x.value) setLang(x.value); }}>{x.label}</button>)}
  </div>;
}
