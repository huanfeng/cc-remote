# web-next conventions

New frontend for cc-remote. Read `docs/redesign-plan.md` (decisions) and `docs/redesign-inventory.md`
(the old frontend's features and protocol — the behaviour you must reproduce) first.

## Stack
React 19, Vite 8, Tailwind v4, `radix-ui` (unified package), cmdk, lucide-react, motion, shiki,
streamdown (+ `@streamdown/code`, `@streamdown/cjk`), zustand, @tanstack/react-virtual, xterm 6.
Package manager: pnpm. Imports use the `@/` alias (→ `src/`).

## Look and feel (Codex / ZCode style)
- Calm, dense, operational. No gradients, no decorative illustration, no marketing spacing.
- **Typography: only `text-ui-xl | text-ui-lg | text-ui-base | text-ui-caption | text-ui-sm | text-ui-xs`**
  (14px base). Never `text-sm`/`text-xs`/`text-[13px]`. Code/terminal content may use their own size.
  Inputs on phones use `text-mobile-input-safe` (16px, prevents iOS zoom).
- **Colours: only semantic tokens** (`src/styles/tokens.css`): surfaces `bg-background`, `bg-sidebar`,
  `bg-card`, `bg-card-selected`, `bg-surface`, `bg-surface-hover`, `bg-selected`, `bg-popover`, `bg-menu`,
  `bg-input`, `bg-user-message-bubble`; text `text-foreground`, `text-foreground-subtle`,
  `text-foreground-subtlest`; states `success|warning|destructive` with `*-surface` and
  `*-surface-foreground`, `bg-progress-surface`, `text-icon-blue`, `bg-brand`; diff `diff-added|removed`;
  borders `border-border` (often `/60`). Never hex values or Tailwind palette colours in components.
- Radii: dialogs `rounded-2xl`, cards `rounded-xl`, rows/menus `rounded-lg`, items `rounded-md`.
- Icons: lucide-react, `size-3.5`/`size-4`. Buttons: `@/components/ui/button` (variants default /
  outline / secondary / ghost / destructive; sizes xs, sm, default, lg, icon-xs … icon-lg).
- Use the primitives in `src/components/ui/*` (dialog, dropdown-menu, popover, tooltip, select,
  tabs, collapsible, command, scroll-area, switch, checkbox, radio-group, textarea, input, badge,
  toast, alert-dialog, hover-card, kbd, spinner) and `src/components/mobile-ui/bottom-sheet`.
  `src/components/ai-elements/*` (conversation, prompt-input, confirmation, queue, plan, shimmer,
  task, chain-of-thought, terminal) are available as building blocks — adapt freely.
- Reference implementation for visuals: ZCode at
  `C:/Users/Cartech/AppData/Local/Temp/claude/e362857efae56196/8a2ab5f7-3c54-4c84-92ee-e07944f1f111/scratchpad/ref/zcode/packages/ui/src`
  (read-only; Apache-2.0). You may copy and adapt code from it: add a header comment
  `// Derived from ZCode (Apache-2.0) <path>; modified by cc-remote.` Never copy ZCode/Z.ai/GLM
  logos, provider/payment/channel icons or sounds.
- Do **not** look at or reuse anything from the old frontend's styles (`web/src/*.css`) — the
  visual design is new. Its *logic* is fair game, and much of it already lives in `src/domain/`.

## Responsive
- `useIsMobile()` (≤767px) switches the app to `MobileShell`. Components receive `mobile` props.
- Touch targets ≥ 36px on phones; dialogs become bottom sheets or full-screen on phones.
- Respect `pb-safe` / `pt-safe` utilities for notches.

## Behaviour rules
- **i18n**: every string is `t('English', '中文')` from `@/lib/i18n`. Default page language is zh.
- **Escape**: a component that handles Escape itself must call `e.preventDefault()`. Radix layers do it
  already. The global "Escape stops the running turn" listener (`useUnclaimedEscape`) ignores
  claimed Escapes. Terminal containers carry `data-terminal`.
- **Data**: never fetch. Use `useBackend()` (`src/backend/backend.ts`). Request bodies must respect
  host features (`hasFeature(info, 'queueControls')` etc. in `src/store/app.ts`).
- **State**: app state `useApp` (`src/store/app.ts`: selection, surfaces/dialogs, sessions, info),
  preferences `usePrefs` (`src/lib/prefs.ts`), transcripts `useTranscripts` (`src/store/transcripts.ts`).
  Open dialogs with `useApp.getState().open({ type: ... })`.
- **Domain logic to reuse** (`src/domain/`, tested): `toolGroups.ts` (groupTranscript, groupStats,
  summaryLabel, argSummary, stabilize, linkRequests, requestSettled, questionViews, parseAnswers,
  compactSummary, isCompacting, formatDuration), `codexStatus.ts` (parseCodexStatus,
  foldCodexActivityEvents, readCodexUsage), `contextReading.ts`, `conversationTime.ts`,
  `filePaths.ts` (splitPaths etc.), `mentions.ts`, `autolinks.ts`, `markdownPreprocess.ts`
  (normalizeClaudeMarkdown, insight blocks), `providers.ts`, `recentProjects.ts`, `clipboard.ts`,
  `terminalPaths.ts` (xterm file-link provider), `notify.ts`.
- Mock data: `src/backend/mock/fixtures.ts` and `mockBackend.ts` (sessions s1–s7 each exercise a
  different case; see comments there).

## Ownership (parallel work)
Each work package owns only its directories. Do not edit files outside them. If you need a change
elsewhere (shared types, mock backend, store), make the smallest additive change only if it is
unavoidable and list it in your final report; prefer local adapters.

## Verification
- `pnpm exec tsc -b` must report no errors **in your files** (others may be mid-edit; ignore theirs).
- `pnpm test` keeps passing.
- Do not use the Playwright browser (it is shared); the coordinator does visual QA.
- No placeholder TODOs, no stubbed branches: implement the behaviour or report what is missing.
