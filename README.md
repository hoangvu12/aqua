# Aqua — Remote VALORANT Agent Picker

Pick and lock your VALORANT agent from your phone, plus an auto-locking **pre-pick**.
Your phone is a remote control for `Aqua.exe` running on your PC; they talk through a
Cloudflare Worker relay at **`aqua.nguyenvu.dev`**. Riot tokens never leave the PC — only
intents (`select Jett`) and game-derived state cross the relay.

```
PC: Aqua.exe (Go)            Cloudflare (aqua.nguyenvu.dev)        Phone (PWA)
  poll VALORANT ── Riot APIs    ┌──────────────────────┐
  relay client  ───outbound────►│ Worker + RelayRoom DO │◄── wss ── SPA
  console UI: QR + status       │ serves SPA + relays   │   https page load
```

> ⚠️ **Ban risk:** auto-select/lock is automation Riot can ban for (instalocker class).
> The read-only parts are low risk; the lock is not. Use at your own risk.

## Layout
- `pc/` — Go app → single static `Aqua.exe` (`internal/{config,riot,picker,relay,ui}`, `cmd/aqua`).
- `cloud/aqua-agent-picker-worker/` — TypeScript Worker + `RelayRoom` Durable Object; also
  mirrors the valorant-api catalog/art at `/api` and `/cdn`.
- `web/` — Vite + React + Tailwind + shadcn SPA (build → `web/dist`, served by the Worker).
- `brand/favicon/` — app icons / PWA manifest source.
- `REMOTE_AGENT_PICKER_PLAN.md`, `PRODUCT.md`, `DESIGN.md` — full design context.

## Build

Toolchain is **Bun** (npm breaks here on sharp's postinstall) + **Go 1.25+**.

```powershell
powershell -ExecutionPolicy Bypass -File build.ps1
```

This builds the SPA (`web/dist`) and `Aqua.exe`. The script also best-effort embeds the
Windows icon via `goversioninfo` (skipped cleanly if offline). To build pieces by hand:

```powershell
cd web;  bun install; bun run build           # SPA
cd pc;   go build -o ../Aqua.exe ./cmd/aqua   # console app — NOT -H windowsgui
```

## Deploy the relay + SPA

```powershell
cd cloud/aqua-agent-picker-worker
bun run deploy        # publishes the Worker + web/dist, binds aqua.nguyenvu.dev
```

`wrangler` must be logged in and `nguyenvu.dev` must be a zone in your Cloudflare account.

## Run

1. Start `Aqua.exe` on your PC. The console shows a **pairing QR**, the pair URL, and an
   8-char code.
2. On your phone, scan the QR (or open the URL) to pair — the phone stores a token and can
   reconnect later without re-pairing. Optionally Add to Home Screen (it's a PWA).
3. Open VALORANT. Aqua reads the current state immediately (lobby / queue / agent select /
   in-match), so you can launch it at any point.

### Console keys
| key | action |
|-----|--------|
| `r` | toggle remote control on/off (dial out to the relay or not) |
| `u` | unpair all phones (clears tokens, kicks connected phones) |
| `q` | quit (Ctrl-C also works) |

Run `Aqua.exe -headless` to skip the console UI and log to stderr (used by the integration
tests / scripted pairing). Config lives at `%APPDATA%\Aqua\config.json`; logs at
`%APPDATA%\Aqua\aqua.log`.

## Develop

- **UI without the PC/Worker:** `cd web && bun run mock` then open
  `http://127.0.0.1:9912/?code=DEV12345&device=devbox` — a local relay speaking the real
  protocol with a scripted game timeline.
- **Worker dev:** `cd cloud/aqua-agent-picker-worker && bun run dev` (`wrangler dev`).
- **Tests:** `go test ./...` in `pc/`; protocol suites `bun test-pairing.ts` /
  `test-picker.ts` / `test-autolock.ts` against `bun run dev`.

> Known env issue: on some Windows boxes `wrangler dev`/workerd accepts TCP but never answers
> HTTP. Use the `bun run mock` harness for UI work and a working box for the Worker.
