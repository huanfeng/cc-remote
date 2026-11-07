# Notices

This directory (`web-next/`) contains code derived from third-party projects.

## ZCode
- Source: https://github.com/zai-org/ZCode (commit `a9fe401eede6e6e5e01be9048679c206fe6f02d9`)
- License: Apache License 2.0 — full text in `third_party/ZCODE-LICENSE.txt`
- Copyright: the ZCode authors (Z.ai)
- Derived files (modified by cc-remote, each marked in its header or listed here):
  - `src/styles/tokens.css` — design tokens from `packages/ui/src/styles.css`; product-specific
    tokens removed, themes re-keyed to `.dark` and `[data-palette]`.
  - `src/styles/index.css` — scrollbar, shimmer and progress fragments.
  - `src/components/ui/*` — primitives from `packages/ui/src/components/ui`
    (`kbd.tsx` rewritten; `radio-group.tsx` switched to the `radix-ui` package).
  - `src/components/lib/utils.ts`, `src/components/mobile-ui/*`, `src/terminal/terminal*.ts`.
- ZCode, Z.ai and GLM names, logos and brand assets are **not** included.

## shadcn/ui
- `src/styles/shadcn-variants.css` and the primitives in `src/components/ui` derive from
  shadcn/ui — MIT License, Copyright (c) 2023 shadcn.

## Vercel AI Elements
- `src/components/ai-elements/*` derive from https://github.com/vercel/ai-elements via ZCode —
  Apache License 2.0, Copyright Vercel, Inc. Per-file headers are preserved.

## Fonts
- Inter (via `@fontsource-variable/inter`) — SIL Open Font License 1.1, Copyright 2016 The Inter
  Project Authors (https://github.com/rsms/inter).
- Noto Sans SC (via `@fontsource-variable/noto-sans-sc`) — SIL Open Font License 1.1,
  Copyright Google Inc. The license texts ship in each package's `LICENSE`.
