# Aqua — Product Context

> Distilled from `REMOTE_AGENT_PICKER_PLAN.md` (the authoritative plan). If the two
> disagree, the plan wins. Keep this in sync when §Phone UI changes.

## Register

**product** — the design serves a task. The user is mid-match, glancing at their phone to
pick or lock a VALORANT agent. The tool must disappear into that one fast action.

## Product purpose

Pick / lock your VALORANT agent from your phone over the internet, plus an auto-locking
**pre-pick**. The phone is a remote control for a Go app (`Aqua.exe`) running on the user's
PC, relayed through a Cloudflare Worker at `aqua.nguyenvu.dev`. The game is the single source
of truth; the phone shows pushed game state and sends intents (`select`, `lock`, config).

## Users

One user: the PC owner, controlling their own machine. No multi-friend rooms, no accounts.
A VALORANT player who:
- is in or about to enter agent select and wants to lock from their phone, or
- wants to **arm a pre-pick** so a chosen agent auto-locks the moment pregame opens.
Bilingual: Vietnamese + English, auto-detected from the game locale, manually togglable.

## The scene (forces the theme)

A player sitting at their PC mid agent-select, phone in hand, ~20 seconds on the pregame
countdown, room lit by the monitor. They need to find an agent in a 3-column grid and commit
in one decisive tap-and-hold. High stakes, low time, zero patience for chrome. → **dark**,
committed VALORANT-red for the one action that matters, real agent art carrying the color.

## Brand / tone

Riot-native dark. Anchors: the in-game VALORANT agent-select screen, the Riot Client, and the
Riot Mobile app. Confident, terse, competitive. Real agent portraits and per-agent gradients
(from valorant-api.com) are the palette; the UI chrome stays quiet so the art reads.

## Anti-references (do NOT look like these)

- Generic SaaS dashboards / hero-metric cards.
- Glassmorphism, gradient text, side-stripe accents, decorative modals (all banned in the plan).
- Pure black (`#000`) backgrounds — always a tinted near-black.
- A settings-panel feel. This is a controller, not a config screen.

## Strategic principles

1. **The game is truth.** The UI is optimistic on POST but always reconciles to the next
   pushed `state`. Never invent state the game didn't confirm (esp. "locked").
2. **One screen, one decisive action.** Everything supports finding an agent and committing.
   The action button morphs to the moment (arm pre-pick → hold-to-lock).
3. **Always legible at a glance.** Connection status, game phase, and countdown are always
   visible. The user should never wonder "is this thing connected / did my lock land."
4. **Ban-risk honesty.** Auto-lock is automation Riot can ban for. The UI states this where
   the user arms it; it never hides what it's doing.
5. **Real art over invented decoration.** Color comes from agent gradients and map splashes,
   under a dark scrim — not from gradients we draw.

## Platform

Mobile-only PWA (installable, "Add to Home Screen"). Portrait phone viewports only. Touch
targets ≥48px. Served by the Worker as a Vite + React + TS + Tailwind + shadcn SPA.
