import { useEffect, useState } from 'react';
import { Select } from './Select';
import { EFFORT_NOTES, loadModels, modelEfforts, type Api, type ModelList } from './ModelPicker';
import type { Provider } from './providers';

export function EffortPicker({ api, provider, model, sessionId, value, onChange, label, inheritLabel }: { api: Api; provider: Provider; model: string; sessionId?: string; value: string; onChange: (value: string) => void; label: string; inheritLabel: string }) {
  const [loaded, setLoaded] = useState<{ api: Api; provider: Provider; sessionId?: string; list: ModelList }>();
  useEffect(() => {
    let active = true;
    if (provider === 'codex') loadModels(api, sessionId, false, provider).then(list => { if (active) setLoaded({ api, provider, sessionId, list }); }).catch(() => {});
    return () => { active = false; };
  }, [api, provider, sessionId]);
  const list = loaded?.api === api && loaded.provider === provider && loaded.sessionId === sessionId ? loaded.list : undefined;
  const supported = modelEfforts(list, model, provider);
  const options = [{ value: '', label: inheritLabel }, ...supported.map(value => ({ value, label: value, description: EFFORT_NOTES[value] }))];
  // Retain previously saved choices while changing models, even if this catalog
  // omits them; users can explicitly choose inheritance or another effort.
  if (value && !options.some(option => option.value === value)) options.push({ value, label: value });
  return <Select label={label} value={value} onChange={onChange} options={options} />;
}
