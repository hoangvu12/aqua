# Aqua — Design System

> Distilled from `REMOTE_AGENT_PICKER_PLAN.md` §Phone UI. Tokens are the contract; theme
> Tailwind v4 + shadcn to these. Real agent/map art carries the saturated color — chrome
> stays quiet.

## Color strategy

**Committed** on a single axis: tinted near-black neutrals + one committed VALORANT-red accent
reserved for selection, the LOCK action, active states, and the live/connection dot. Real
agent gradients and map splashes (valorant-api.com) supply the rest of the color, always under
a dark gradient scrim (never glass).

## Tokens (OKLCH)

| token | value | use |
|---|---|---|
| `--bg` | `oklch(0.16 0.01 270)` | background (never `#000`) |
| `--surface` | `oklch(0.20 0.011 270)` | cards |
| `--surface-hi` | `oklch(0.24 0.011 270)` | raised / pressed |
| `--hairline` | `oklch(0.32 0.012 270)` | 1px full borders (no side-stripes) |
| `--text` | `oklch(0.97 0.005 270)` | primary text (off-white) |
| `--text-dim` | `oklch(0.72 0.01 270)` | secondary |
| `--text-mute` | `oklch(0.55 0.01 270)` | tertiary / labels |
| `--accent` | `oklch(0.63 0.22 18)` ≈ `#FF4655` | selection ring, LOCK, active, live dot |
| `--accent-hi` | `oklch(0.69 0.22 18)` | accent hover / highlight |
| `--ok` | `oklch(0.72 0.14 150)` | connected |
| `--warn` | `oklch(0.78 0.14 75)` | reconnecting |

Tint every neutral toward hue 270. No `#000`/`#fff`.

## Typography

- System stack / Inter: `Inter, system-ui, -apple-system, "Segoe UI", sans-serif`. One family.
- Bold headings; scale ratio ~1.2; **fixed rem** (no fluid clamp — consistent phone DPI).
- Body/prose 65–75ch (rarely hit on a controller). Labels uppercase + tracked for the
  game-select feel, used sparingly.

## Elevation & shape

- Radius: 16 (cards / large), 12 (controls / tiles), pill (chips / role filters).
- Borders: 1px full `--hairline`. No side-stripes. No nested cards.
- Depth via the surface ladder (`--bg` → `--surface` → `--surface-hi`), not heavy shadows.

## Motion

- 150–220ms, ease-out-quart (no bounce/elastic). Respect `prefers-reduced-motion`.
- Motion conveys state only: selection ring, hold-to-lock fill, status chip transitions,
  state-screen swaps. The hold-to-lock fill is the one signature motion (≈600ms progress).
- Never animate layout properties; transform/opacity only.

## Components

- **Connection chip**: dot + label. ok = `--ok`, reconnecting = `--warn`, offline/error = `--text-mute`/`--accent`.
- **Agent tile** (grid, 5-col): `displayIcon` only (perf). States: default, selected
  (`--accent` ring + ✓), disabled-not-owned, disabled-taken, disabled-locked — each disabled
  reason gets a **distinct glyph**, not just opacity.
- **Role filter pills**: pill row, single-select + "all".
- **Allies strip**: read-only seats, small avatars + status.
- **Action bar button**: morphs by phase. Pre-game → "Arm pre-pick · {AGENT}". Pregame →
  hold-to-lock "{AGENT}" (600ms fill → optimistic `locking` → settles to `locked` on the next
  confirmed game state). Auto-lock toggle adjacent.
- Every interactive control ships default / pressed / disabled / (focus for a11y).

## Imagery

- Agents: `displayIcon` (tiles), `fullPortrait` (selected/locked hero), `backgroundGradientColors`
  (4× RRGGBBAA → CSS `#rrggbbaa`, last stop alpha `00`). `role.displayIcon`.
- Maps: `splash` as the status-zone background under a dark gradient scrim; join GLZ `MapID`
  to `mapUrl` (not display name).
- Lazy-load portraits/splash; grid uses icons only. Cache catalog in `localStorage` keyed by
  valorant-api `/v1/version`.

## Bans (in addition to shared absolute bans)

No glassmorphism, no gradient text, no side-stripe borders, no decorative modals, no `#000`.
Pairing is a full-screen first-run **view**, not a modal.
