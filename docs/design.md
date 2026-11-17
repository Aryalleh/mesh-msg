# Mava design system

One look for the handheld (LVGL, `firmware/src/ui`), the web client (`web/static`) and the
board's setup panel (`firmware/src/net.cpp`). Chosen with the UI UX Pro Max skill
(design system "Dark Mode (OLED)" + "Cyberpunk Mobile HUD"), adapted for Persian, small
screens and low power: dark OLED base, neon violet, restrained cyberpunk effects.

## Tokens

| Token | Value | Use |
|---|---|---|
| `bg` | `#0B0A14` | page / screen (near-black, violet tint: OLED friendly) |
| `surface` / `surface-2` | `#15122A` / `#1E1A3A` | cards, status bar / buttons, keys, their bubbles |
| `border` | `#3B2A6B` | 1 px edges |
| `fg` / `muted` | `#ECE8FF` / `#A9A2CF` | text (≥ 12:1 / ≥ 7:1 on `bg`) |
| `primary` / `primary-hi` | `#7C3AED` / `#A78BFA` | buttons, pressed keys / titles, links, wordmark |
| `accent` | `#D946EF` | focus edge, status-bar rule, gradient end |
| `ok` | `#22D3EE` | link up (always with a word: «وصل») |
| `danger` | `#FF4D6D` | destructive / errors |
| `mine` | `#2E1A5E` | my chat bubbles |

## Type

- **Vazirmatn** (OFL) for all text — Persian and Latin, 16 px body, 1.5–1.6 line height.
  Web: self-hosted woff2 (`web/static/fonts`), handheld: `src/ui/fonts/mava_font_16.c`.
- **Orbitron** 700 (OFL) only for the **MAVA** wordmark (web, panel), letter-spaced, glow.
- Monospace (`ui-monospace`) for identities, links, times.

## Effects (cyberpunk, restrained)

- Chamfered corners (`clip-path` 10 px) on cards, inputs, buttons, chips, bubbles; LVGL
  uses 2 px radius (no clip paths).
- Glow only where it means something: primary button, focused field, focused item
  (keypad / trackpad focus on the handheld = magenta edge + violet shadow).
- Faint CRT scanlines (2.5 % white, 3 px) over the web page, off with reduced motion.
- Radial violet haze at the top of the page / vertical gradient on the handheld screen.

## Rules (from the skill's checklist)

- SVG icons (Lucide shapes in an inline sprite) — no emoji as icons; LVGL symbols on the
  handheld.
- Touch targets ≥ 44 px (web buttons 48 px, inputs 48 px); visible labels on every field;
  errors as a toast near the action with a fix, never `alert`.
- Status never by colour alone: link chips show icon + word («برد وصل», «رله قطع»).
- `prefers-reduced-motion` turns off animations and scanlines; animations 150–300 ms,
  only new chat messages animate in.
- Safe areas respected (header, composer); 16 px inputs (no iOS zoom).
