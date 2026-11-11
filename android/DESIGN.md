# mesh-msg Android — design system

Source of truth for the app's UI. Built with the `ui-ux-pro-max` design skill: its product
match for *Chat & Messaging App* (style **Minimalism + micro-interactions**; palette
*messenger blue + online green*), its *VPN & Privacy Tool* dark palette, its Jetpack Compose
stack rules (Material 3 tokens, `colorScheme`, immutable state) and its accessibility /
touch / navigation rule set. Where the skill's generic output did not fit (landing-page
patterns, display fonts) the rule it is derived from was applied instead; those choices are
marked *(adapted)*.

## Who uses it, and where

People in an internet shutdown: stressed, often outdoors or at night, on cheap or old
phones (Android 8+, small screens, low RAM), reading Persian (RTL) or English. The app may be
looked at over their shoulder. So: calm, quiet, legible, fast, nothing decorative — and
always honest about whether a message actually went anywhere.

## Principles

1. **Status is content.** Every message shows how far it got (queued → sent to the mesh →
   delivered); the network screen says in words which paths exist right now. Never colour alone:
   an icon and a text label carry the same information.
2. **Dark first.** Dark is the default (night use, OLED power, discretion); light is a full
   peer, designed together, both checked for contrast.
3. **One primary action per screen.** Send in a chat; add a contact in the list.
4. **Works on the worst phone.** System fonts (no font files: smaller APK to share phone to
   phone), no blur, no gradients, no images, lazy lists, animations ≤ 250 ms and off when the
   system's animation scale is 0.
5. **RTL is not a mode.** Layouts use start/end only; bubbles, icons with direction and
   timestamps mirror; numbers follow the locale.

## Colour tokens

Semantic tokens only — no hex in screens. Mapped onto the Material 3 `ColorScheme`.

| Token | Dark | Light | Use |
|---|---|---|---|
| `background` | `#0B1120` | `#FFFFFF` | screen |
| `surface` | `#0F172A` | `#FFFFFF` | app bars, sheets |
| `surfaceContainer` | `#192134` | `#F1F5FD` | cards, incoming bubble, inputs |
| `surfaceContainerHigh` | `#232D45` | `#E4ECFC` | pressed / selected rows |
| `onSurface` | `#F1F5F9` | `#0F172A` | primary text (17.1:1 / 17.9:1) |
| `onSurfaceVariant` | `#94A3B8` | `#526077` | secondary text (7.0:1 / 6.3:1) |
| `outline` | `#33415C` | `#C5D2EA` | dividers, field borders |
| `primary` | `#8AB4FF` | `#2563EB` | actions, links, own bubble (light) |
| `onPrimary` | `#0A1A3D` | `#FFFFFF` | text on primary |
| `primaryContainer` | `#1E3A8A` | `#DBE7FF` | own bubble (dark), selected chips |
| `onPrimaryContainer` | `#E3ECFF` | `#0B2A6B` | text in own bubble (10.6:1 / 11.4:1) |
| `tertiary` (success) | `#4ADE80` | `#047857` | delivered, link up |
| `warning` | `#FBBF24` | `#B45309` | waiting, limited path |
| `error` | `#F87171` | `#DC2626` | failed, destructive |

Dark values are lighter, less saturated tonal variants of the light brand colours, not
inversions (skill rule `color-dark-mode`). The background is a deep navy rather than pure
black: pure `#000` smears on OLED when scrolling and loses the elevation steps.

## Type

System font (Roboto / Noto Sans Arabic for Persian — the skill's RTL recommendation, which is
what Android already ships) *(adapted: no bundled fonts)*. Material 3 roles, sizes in `sp` so
the user's font scale applies; nothing below 12 sp; body line height 1.5.

| Role | Size / line | Weight | Use |
|---|---|---|---|
| `titleLarge` | 22 / 28 | 600 | screen titles |
| `titleMedium` | 16 / 24 | 600 | contact names, section headers |
| `bodyLarge` | 16 / 24 | 400 | message text, inputs |
| `bodyMedium` | 14 / 21 | 400 | previews, descriptions |
| `labelLarge` | 14 / 20 | 500 | buttons |
| `labelSmall` | 12 / 16 | 500 | timestamps, badges (tabular figures) |

Identity hashes and links use the monospace system font.

## Space, shape, elevation

4 dp grid: 4 · 8 · 12 · 16 · 24 · 32. Screen gutter 16 dp. Row height ≥ 72 dp for chat rows,
≥ 56 dp for settings rows. Every touch target ≥ 48 × 48 dp with ≥ 8 dp between targets.
Corners: 20 dp bubbles (4 dp on the tail corner), 16 dp cards, 28 dp input pill, full for
avatars and FAB. No shadows; hierarchy comes from the surface steps above.

## Components

- **Chat row** — avatar (initial on a colour derived from the contact hash, so it is stable
  and needs no image), name, one-line preview, time; unread count badge. Whole row is one
  target.
- **Message bubble** — own: `primaryContainer`, end-aligned; theirs: `surfaceContainer`,
  start-aligned; max width 82 %. Footer: time + delivery mark for own messages:
  clock = queued on this phone, single check = handed to the mesh / relay, double check =
  delivery receipt came back. Each mark has a content description.
- **Composer** — pill field with a visible send button (48 dp); send disabled while empty; a
  byte counter appears near the LoRa-friendly limit, because short texts travel further.
- **Path chips** (network screen) — one per transport (nearby phones, LoRa node, internet
  relay): icon + name + plain-words state. Green/amber/grey dot *and* text.
- **Empty states** — one sentence on what this screen will show and the one action that
  fills it.
- **Destructive actions** (remove contact, panic wipe) — error colour, separated from other
  actions, always confirmed; wipe needs a typed confirmation.

## Navigation

Bottom bar, three top-level destinations, icon + label: **Chats · Network · Settings**.
Chat and Add contact are pushed screens with a top app bar and back arrow (mirrored in RTL);
system back / predictive back always works. State (scroll, draft) survives navigation.

## Motion

150–250 ms, ease-out in / ease-in out; screen change = short fade + 8 % slide in the reading
direction; new message = fade + 12 dp rise. Nothing loops. All of it is skipped when the
system animator duration scale is 0.

## Voice

Plain words, no jargon in primary text ("Nearby phones", not "BLE GATT peers"); technical
detail is available one tap deeper. Never say "sent" for a message that has only been
queued. Persian strings are written, not machine-mirrored English.

## Icons

Material Symbols (outlined, 24 dp, one family) as vectors. No emoji as icons.

## Checklist for every screen

- [ ] contrast: text ≥ 4.5:1, icons / large text ≥ 3:1, both themes
- [ ] 48 dp targets, 8 dp apart; content clear of system bars and the keyboard
- [ ] works at font scale 2.0 and at 320 dp width without clipping
- [ ] mirrored correctly in RTL
- [ ] every icon-only control has a content description
- [ ] state is never colour-only
- [ ] no animation when the system disables animations
