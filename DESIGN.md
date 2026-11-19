# Design

## Source of truth
- Status: Active
- Last refreshed: 2026-11-15
- Primary product surfaces: session creation, conversation header, configuration, subscription usage, desktop-owned system tray.
- Evidence reviewed: web/src/App.tsx, style.css, ModelPicker.tsx, SessionStatus.tsx, Usage.tsx; docs/codex.md and docs/architecture.md.

## Brand
- Calm, compact, practical remote coding workspace; reuse existing colors and controls.
- Trust comes from showing reported execution settings separately from pending choices.
- Avoid unnecessary panels, decorative elements and implementation details in the main flow.
- Application mark: two separate uppercase C letterforms, following the user's revised choice of the CC monogram. Keep a visible gap between them rather than interlocking their strokes. Retain the dark green tile (`#222c27`) and light green mark (`#d6ebbc`); use matching rounded strokes that remain recognizable at 16–32 px. Draw the letters as vector outlines without a font dependency. Source and regeneration notes: docs/brand.md.
- The `logo` host preset follows the application mark. Empty host icons use it; preserve explicitly chosen device/code presets, custom text and emoji. Keep web, host-generated favicons, desktop bundles and the native tray consistent.

## Public introduction and onboarding (2026-11-15)

- Evidence: README.md, README.en.md, docs/usage.md, docs/desktop.md, docs/codex.md, web/public/icon.svg and the existing Pages workflow. The user wants clearer promotion and first-use guidance. The current Pages root is a working client with browser-local host credentials; keep its URL and storage origin unchanged.
- Audience: developers already using Claude Code or Codex who want to continue work from a phone/browser or compare subscription accounts. Lead with the job and an existing product screenshot, then a concrete installation path. Explain that execution stays on the user's host and remote access needs a network connection; do not promise hosted execution or universal terminal handoff.
- README hierarchy: value proposition, a few concrete use cases, platform/provider scope, three-step first use, everyday commands, task-oriented documentation and concise troubleshooting. Keep Chinese and English equivalent; move operational detail into linked guides.
- A static introduction lives at `/about/`, with English at `/about/en/`; the existing web client stays at `/`. Use the current dark green icon, warm neutral surfaces, green accents, system fonts and the supplied screenshots (identified as interface examples). Avoid fabricated usage metrics, testimonials, gradients and external tracking or fonts.
- Landing-page hierarchy: product navigation, task-focused headline and install/docs actions, screenshot, a short terminal-to-phone workflow, capabilities with accurate Claude/Codex scope, OS-specific installation commands, and guide links. No authentication or host/token forms. No local/session storage access.
- Responsive/accessibility: readable single-column phone layout, no horizontal page overflow, visible keyboard focus, semantic headings/navigation, meaningful image alternatives, selectable wrapped commands and copy feedback with a non-clipboard fallback. Copy controls must be keyboard operable; use no decorative motion.
- Implementation: buildless HTML/CSS/JS, no new dependencies. A small Node build step stages the introduction and existing images under the Pages artifact's `about/` directory only. Preserve the existing client build, API behavior and release archives. Verify relative links, both languages, command content, keyboard/copy interactions, desktop/mobile screenshots and existing standalone build.
- Assumption: add a companion introduction rather than replacing the client's established public URL. No open questions block the documentation work.

## Product goals
- Switch Claude and Codex within the same projects with minimal repeated input.
- Make provider, permissions, model and reasoning effort discoverable before and during a task.
- Show the current provider's subscription quota near the conversation.
- Non-goals: changing authentication, billing, or the native terminal workflow.
- Success: shared recent paths, explicit Full Access, working next-turn configuration changes, accurate quota ownership.

## Personas and jobs
- Developers controlling host sessions from desktop and mobile browsers.
- Create a task, change its configuration, inspect progress and remaining subscription quota.

## Information architecture

- OAuth account editing keeps its stable internal ID, allows name-only edits, and treats a blank edit-token field as keep existing. Display the host's validation detail inline rather than replacing it with a generic request failure. Renaming does not rewrite saved defaults or session snapshots; the editor explains manual reselection where needed.

- Terminal request failures: explicit Claude API error records render as errors and end the current turn as Failed even without a Stop hook. Full selected-model validation diagnostics have a screen fallback. A late Stop cannot hide the failure; retry spinners and quota menus keep their existing states. A new submission clears the failed-turn guard, and held messages are not automatically retried after failure.

- Session task panels place one cross-type Running group above the existing subagent, background-command and teammate groups. Active and pending work appears once at the top with its task type; finished work returns to its type group. Desktop and mobile use the same grouping.
- Desktop HTTP/HTTPS links open in the system browser, including modified clicks and terminal links. In-app routes retain app navigation; failures to launch the browser provide visible feedback.

- Dialog dismissal: browser/phone Back closes the topmost open dialog or mobile drawer before changing the conversation route. Desktop Ctrl/⌘+W closes that same layer before the native tab; unsaved-change confirmation remains effective. Explicit closing removes the temporary history guard, and nested confirmations close before their parent. Browser-reserved tab-closing shortcuts remain controlled by the browser.

- Settings (2026-11-18): dedicated Claude Code and Codex sections contain their own supplier and session defaults; Claude Code additionally contains native settings and status line controls. Suppliers combines official Claude OAuth, local Claude/Codex API connections and read-only CC-Switch imports. Server describes the connected server; Appearance & interaction contains local client preferences. Desktop retains Local service and About & updates. Preserve old defaults/oauth shortcuts via aliases and unsaved-change guards on mobile and desktop.
- Suppliers is the first settings section and the default desktop destination. Supplier editing uses a separate, directly navigable editor for connection/account, models/options and nonsecret connection JSON; no sequential step gating. Adding offers official/custom API templates. OAuth uses a single form with optional setup instructions. Save is explicit; cancellation clears unsaved credentials.
- Supplier editing uses write-only password fields, never existing credentials or credential-bearing endpoint URLs. Leave empty to retain an existing credential. New supplier/statusline options are optional; old hosts retain OAuth, imports and core settings flows with scoped upgrade notices.
- Supplier forms use grouped sections and vertical label/control/helper fields with consistent control heights. Model inputs remain plain text fields with a separate searchable picker. Connection tests sit next to the URL; model discovery sits in the models heading with a single reserved status line. Claude mappings use one row per role (Sonnet, Opus, Haiku, Fable), with columns for display name, actual request model and 1M declaration. Narrow layouts scroll the table horizontally. Context declarations use upstream-compatible [1M] suffixes; the default fallback model follows the mappings, before advanced options. Custom Claude connections default to Auth Token; official templates and existing saved authentication retain their explicit choice.
- Sidebar: navigation, sessions and host actions.
- New-session dialog: provider cards, supplier, session type, project, permissions/model/effort.
- Conversation header: identity, task state and account quota; model/effort switches in status details. Composer: one control row and context ring, with pending messages above it.

## Design principles
- Reuse existing components and spacing.
- Preserve manually entered paths when switching provider.
- Show unknown values explicitly; never derive subscription quota from token counts.
- Changes during execution apply to a subsequent turn; keep the current task running.

## Visual language
- Keep existing CSS variables, typography, radii, borders and icon sizes.
- Provider options are two vertically stacked cards with active selection and short descriptions.
- Usage is a compact two-row header control with period badges, thin bars, percentages and reset countdowns.
- No new imagery or motion.

## Components
- Reuse mode-choice, Select, ModelPicker, QuickSwitch and the usage details panel.
- Reported settings belong to the shared status dropdown; session context has a separate composer ring.
- Keep provider-specific permissions and quotas attached to the selected provider.

## Accessibility
- Native buttons, visible focus, explicit labels and selected states.
- Provider card selection supports keyboard navigation and screen readers.
- Quota percentages include readable text and detailed tooltips.

## Responsive behavior
- Preserve current desktop/mobile breakpoints.
- Provider cards stack at every width; dialog can scroll on small screens.
- Header settings and quota wrap or compact without horizontal overflow.
- Touch users can open the same quota and configuration details.

## Interaction states
- Collapsed sidebar: both the welcome page and conversation header retain an explicit expand button, including after restoring saved preferences; the welcome page reuses its existing 48px drag strip.
- Loading: neutral quota placeholders and existing model loading indicators.
- Empty: explain absent login, API-key billing or unavailable quota.
- Error/offline: retain scoped cached readings and mark them stale.
- Success: next-turn configuration visible without interrupting current execution.
- Disabled: unavailable providers remain clearly identified; legacy hosts stay Claude-only.

## Content voice
- Concise Chinese and English using existing t() patterns.
- Use Full Access / 完全访问 for Codex unrestricted execution.
- Distinguish reported configuration, saved choice, subscription quota and session tokens.

## Implementation constraints
- Existing React/TypeScript and Go stack; no new dependencies.
- Use Codex app-server APIs and returned model capabilities.
- Targeted protocol tests, frontend state/render tests, typecheck/build and responsive UI inspection.

## Open questions
- None blocking. Usage defaults to the active provider; host details allow selecting either provider.

## Selected composer revision (2026-10-27)
- User accepts the overall layout with these overrides: subscription 5h/7d quota belongs in the topbar, using the supplied compact badge/bar/percentage/reset-time reference.
- Composer toolbar stays on one row at every width: attachment icon, slash, permissions (icon when space is tight), flexible space, current-session context ring, send split-button.
- Context ring opens context occupancy/capacity and session token details; account quota must not appear in this ring. Missing context readings remain unknown, never inferred from cumulative usage.
- Model and effort switching remain available in the topbar status dropdown together with actual and pending configuration. All provider surfaces use the same layout and existing visual style.
- This revision supersedes the earlier A/B/C placement proposals. The approved layout is implemented in the shared Claude/Codex components. Pending messages have stable IDs and atomic host queue actions. Already-delivered messages remain read-only; uncertain delivery is never automatically resent.

- Approved throwaway HTML reference captured on branch `prototype/composer-20261027` (`web/composer-prototype.html`); production uses the React components, not the prototype route.

## Interface refinements (2026-10-27)
- Keep account quota in the topbar as compact text only, including reset countdowns; hide unsupported subscription quota.
- Retain the established bordered styling for attachment, command and permission controls. Show concise provider-specific permission names, with anchored in-app confirmation for risky choices.

## Session navigation refinements (2026-11-13)

- Evidence: features/header/ModelChips.tsx, features/pickers/QuickSwitch.tsx, features/status/SessionDetails.tsx, features/workspace/SessionHeader.tsx, features/files/FileViewer.tsx and features/sidebar/Sidebar.tsx under web/src. Reuse the existing supplier catalog, switch endpoint, Radix menus and typography tokens.
- Supplier chips open a directly anchored supplier picker, like model and effort. Keep provider-scoped options, native/default labels, unsupported-host feedback and the backend's confirmation before interrupting live work. Do not route a simple switch through session details.
- Enabled buttons and menu actions use a pointer cursor; disabled actions retain a noninteractive appearance. Keep visible keyboard focus and existing mobile picker surfaces.
- Session directories and file-preview paths open a menu: full-path copy, name copy and parent-directory copy. In the desktop client, an absolute path on a loopback host additionally offers opening the local folder, revealing the path in the system file manager and opening a terminal in that folder. Remote host paths remain copyable; do not interpret them as local filesystem targets. Revalidate native operations in Rust and pass paths as arguments/current directories, never shell command text.
- Project headings use a larger, stronger existing type token and taller hit areas. Add a virtual “In progress / 进行中” group before projects for running, waiting and background work, with project context on every row. Keep original project membership, filtering and archived behavior; derive shortcuts before pagination so older active sessions stay reachable. The virtual group's collapsed state is separate from project path preferences.
- Verify supplier choices/interruption behavior, local-path validation/command construction, live grouping/search/pagination, typecheck/build and responsive visual states. No new dependencies.
- The session status popover exposes model, reasoning and supplier controls; supplier changes reuse the existing active-work interruption safeguard.

## Frontend subscription overview (2026-11-14)

- Evidence: web/src/features/status/UsageMonitor.tsx and backend/types.ts. The existing account dropdown hides every other account's current reading.
- Render all monitored accounts together as compact cards, two columns on wider screens and one on phones. Each card shows provider/account identity, collection or error state, plan, update age and every reported quota; retain stopped accounts and cached stale readings with clear labels. Missing or invalid readings stay unknown.
- Keep sampling and refresh shared. A card's history action selects its stable account ID and scrolls to the existing history panel; only that panel uses an account selector. Preserve tray account deep links, history ranges, fetch cancellation and legacy-host fallback. Do not fetch separate histories for every overview card.
- Reuse current bars, warning/success tokens and form controls; no new dependencies. Validate mixed account states, switching history without hiding other accounts, refresh/cadence, account deep links, and desktop/mobile layout.

## Native tray revision (2026-11-13)

Historical design, superseded for CC Remote distribution by the desktop integration below. The old wind-ui-rust tool has moved out of this repository into the sibling `cc-tray/` directory, with its artwork and preview harness; an independent Git repository and standalone provider refresh remain deferred.

### Evidence and intent
- User requested an overall interaction and visual redesign. Confirmed constraints: wind-ui-rust native UI, no resident WebView, backend-owned sampling, account continuity and history; detailed history opens Tauri on demand.
- Historical evidence: the former standalone tray sources (now in `cc-tray/`), docs/subscription-monitor.md and web/src/styles/tokens.css. The original tray presented unstructured text in a centered window with raw timestamps and ambiguous sampling buttons.
- Inferred visual direction: calm, compact utility with a light neutral surface, blue accent, clear typography and quota bars. This is an implementation decision, not a user-supplied visual reference.

### Jobs, hierarchy and behavior
- Primary job: glance at remaining capacity while coding; compare accounts and see when limits reset. No authentication setup or historical charts inside the tray.
- Hover: a 300 dp graphical native preview for every favorite account, with all quota bars, percentages and reset times; delayed by 400 ms, no focus activation, and hidden after leaving the icon/flyout. Scroll when content exceeds the monitor work area. Suppress while the main panel is open. Click: toggle the main panel open/closed. Right-click: native actions menu.
- Panel hierarchy: title and connection state with all-history beside refresh; scrollable compact account cards. Account cards show provider/name, explicit used percentages, reset countdowns and an independent favorite-for-hover control; plan and update age live in the identity tooltip. No sampling controls in this surface.
- All accounts remain accessible; favorites appear first. Stable account IDs own selections, never names or credentials. Favorites support multiple independent toggles and preserve unavailable IDs; read legacy focusedAccount as a single favorite, then persist favoriteAccounts on change. Changing favorites updates the hover preview immediately.
- Success: current readings fit without raw JSON/timestamps; reopening uses cached data immediately; summary/hover never starts Tauri.

### Native visual language and components
- Reuse the product's sky/blue accent, neutral text and existing brand icon. Quota bars communicate reported usage, with amber/red at high utilization; numbers and labels carry the same information without color.
- User selected compact list A over comparison table B and single-account dashboard C, then requested further density. Main panel: 440 dp wide, content-sized height (ordinary single account 168 dp, two accounts 254 dp, capped at 480 dp), 10 px outer spacing, 8 px account padding and 4–6 px gaps. Pair short-term and weekly quotas horizontally; account identity, history and pin share one line. Plan and update age live in the identity tooltip; errors remain visible. Extra quotas/accounts scroll.
- No footer, sampling controls or instructional row. All-account history sits beside refresh in the header. Sampling remains a backend/client setting.
- Quota labels have stable semantic colors: short-term blue, weekly teal, named/model-specific quotas such as Fable purple. Usage bars and percentages independently use amber/red at high utilization. Labels remain readable without color.
- Use the shared CC Remote PNG for tray/window icons and embed the desktop ICO into the executable. The main panel and hover preview are tool windows (WS_EX_TOOLWINDOW), with no taskbar or Alt+Tab entry.
- Main panel stays open when focus changes so it can be dragged and compared with another application. Its title is a native window drag region, with buttons excluded from dragging; close with the tray toggle, close button or Escape. No position persistence yet.
- Use existing windui labels, progress, native controls, scroll containers, theme tokens and focus behavior. No new dependencies. Main owns lifecycle; panel owns composition; lib owns presentation formatting and favorite persistence.
- Keep unrelated web/Tauri design decisions above intact. This revision governs only native tray surfaces.

### Accessibility, sizing and states
- Readable Chinese system font, clear text contrast, keyboard-reachable buttons, explicit favorite selection and Escape dismissal. No hover-only commands.
- Place within the current monitor work area and accommodate taskbar placement/DPI. Scroll account content, keep primary actions visible. Do not treat hover as available on touch.
- Loading: connect indicator and descriptive placeholder. Empty: explain login/account setup and offer the client. Offline: retain last readings, mark them stale and offer retry. Account errors remain scoped to that account.
- Busy: animate the fixed-width refresh/reconnect button in the header and prevent duplicate manual submissions. Never insert a busy/result block into the account list. Manual success/error feedback uses windui host toasts (3 s success, 5 s error), outside layout flow; background cache polling stays quiet. Do not replay expired feedback when reopening. Persistent connection/account state remains visible. Favorite state changes only after local persistence succeeds.
- Format age/reset as readable relative time; unknown readings stay unknown. Percentages mean used subscription capacity, never token counts or billing. Unsupported identity explains history limitations only where relevant.

### Validation and open questions
- Target Windows first. Verify formatting/selection/state transitions with targeted Rust tests, then locked build and native rendered screenshots; check account overflow and error/empty states with deterministic fixtures.
- Toolkit limitations must be recorded honestly; no WebView fallback. Follow work-area anchoring where supported by the pinned toolkit and use a minimal Windows adapter only if necessary.
- The pinned toolkit exposes neither tray hover nor nonactivating windows. A Windows-only read-only GDI flyout queries Shell_NotifyIconGetRect for the windui host (pinned host class and icon ID); updates reuse the backend snapshot. No hooks into other processes, no authentication in the flyout. Revalidate this bridge when updating windui.
- Open questions: none blocking; user approved overall redesign rather than a specific visual mockup.

## Desktop tray integration (2026-11-13)

- Desktop owns the Tauri tray, backend cache worker and navigation. Windows hover renders the compact 440 dp account panel with wind-ui-rust in the same process; no second tray, standalone executable or WebView for usage. The independent tool/provider implementation remains deferred.
- Hover waits 400 ms, keeps the panel open while moving from icon into its controls (200 ms leave grace), and closes after leaving both. Close/Escape, history navigation and tray clicks suppress reopening until the cursor leaves. The native tool window shows without activation, supports title dragging and scrolling, and has no taskbar entry. All accounts remain accessible, with multiple favorites first; preserve compact paired quotas, semantic colors, header loading button and layout-free toasts. No sampling controls/footer.
- Left-click opens/restores the CC Remote main window. Right-click offers opening the client, refreshing, all history and quitting. Other platforms retain native tooltip/menu navigation; graphical hover is Windows-only.
- Tauri window creation/activation runs on a dedicated serial worker, never synchronously inside tray/single-instance/IPC callbacks (WebView2 deadlock). windui owns a native UI thread, receives cached snapshots over its application channel, and routes actions to desktop workers. Poll the pointer only during active hover sessions.
- Rust reads the local backend cache every 30 s. Only explicit refresh calls POST /api/usage/monitor/refresh; backend owns OAuth, sampling, stable identity and SQLite history. Read the token file per request; validate loopback origin and disable redirects/proxies.
- Desktop tray-preferences.json imports legacy favoriteAccounts/focusedAccount when absent. Preserve unavailable IDs and explicit empty selection; persist before publishing favorites.
- Main/host windows use native system decorations on Windows/Linux, native traffic lights on macOS. --tray creates no WebView; normal launch/tray click creates main on demand. Closing main hides it, host windows close normally. Quit exits desktop while backend keeps recording. Second normal launch activates main; --tray leaves it hidden.
- CI/installers ship backend plus desktop, migrate startup shortcuts and preserve accounts/history. No standalone tray artifact. Pin the reviewed windui commit so CI and clean builds use identical APIs.
- Verify native panel layout/toasts/actions, hover lifecycle, favorites, cache/manual-refresh and monitor placement; retain startup/repeated activation smoke checks. Windows is the runtime validation target; macOS/Linux runtime remains unverified.
- Refresh uses a small vector arc driven by the native paint clock, rather than cycling text glyphs or changing layout on a timer. Only its icon area requests continuous repaint while busy; idle and reduced-motion states do not animate. Toast hover pauses dismissal while keeping the message fully readable.

- Status line appearance: opt-in short labels P/M/E/C/I/O/D/T, costs keep $, quota labels remain 5h/7d. Distinct multicolor defaults, three whole palettes and individual quick swatches plus custom color. Preserve existing explicit colors; expose short labels only when the host advertises support. Preview mirrors renderer values and thresholds.

- Supplier configuration follows the upstream CC-Switch full-form editing pattern (docs/user-manual/en/2-providers/2.3-edit.md). Keep local metadata, Claude alias mappings/timeouts and Codex connection/model options in cc-remote; no global config editing, routing or proxy. Every public field has a ccr profile show/add/edit counterpart; credentials remain write-only.

Settings enhancements: API suppliers expose model discovery/connection checks through a bounded, redirect-free model-list request. Existing credentials are reused only for the same saved endpoint and authentication. Supplier dialogs retain a fixed size across sections. Native Claude settings and statusline customization open in independent scrolling dialogs, with revision-aware saving and discard guards. Statusline visibility is independent from a persisted complete order, with drag handles and keyboard-accessible movement buttons. CC-Switch API connections can be copied into independent local configurations; disabling its list stops catalog reads, with default migration checks. See docs/ccswitch-migration.md for supported and deferred fields.
