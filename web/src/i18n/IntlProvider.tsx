// Compatibility shim for components derived from ZCode, which call
// useZCodeIntl().intl.formatMessage({ id }). cc-remote writes its messages
// inline as t('English', '中文') pairs (see lib/i18n.ts); the few ids the
// borrowed primitives use are mapped here.
import { t } from "@/lib/i18n.js";

const messages: Record<string, () => string> = {
  "common.close": () => t("Close", "关闭"),
  "common.loading": () => t("Loading", "加载中"),
};

const intl = {
  formatMessage({ id }: { id: string }, values?: Record<string, unknown>): string {
    let text = messages[id]?.() ?? id;
    if (values) for (const [k, v] of Object.entries(values)) text = text.replaceAll(`{${k}}`, String(v));
    return text;
  },
};

export function useZCodeIntl() {
  return { intl };
}
