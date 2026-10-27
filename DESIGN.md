# Design

## Source of truth
- Status: Active
- Last refreshed: 2026-10-27
- Primary product surfaces: session creation, conversation header, configuration, subscription usage.
- Evidence reviewed: web/src/App.tsx, style.css, ModelPicker.tsx, SessionStatus.tsx, Usage.tsx; docs/codex.md and docs/architecture.md.

## Brand
- Calm, compact, practical remote coding workspace; reuse existing colors and controls.
- Trust comes from showing reported execution settings separately from pending choices.
- Avoid unnecessary panels, decorative elements and implementation details in the main flow.

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
- The session status popover exposes model, reasoning and supplier controls; supplier changes reuse the existing active-work interruption safeguard.
