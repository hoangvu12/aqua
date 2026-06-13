# Aqua — Remote VALORANT Agent Picker

Pick and lock your VALORANT agent from your phone, plus an auto-locking **pre-pick**.
Your phone is a remote control for `aqua.exe` running on your PC; they talk through a
Cloudflare Worker relay at **`aqua.nguyenvu.dev`**. Riot tokens never leave the PC — only
intents (`select Jett`) and game-derived state cross the relay.

```
PC: aqua.exe (Go)            Cloudflare (aqua.nguyenvu.dev)        Phone (PWA)
  poll VALORANT ── Riot APIs    ┌──────────────────────┐
  relay client  ───outbound────►│ Worker + RelayRoom DO │◄── wss ── SPA
  console UI: QR + status       │ serves SPA + relays   │   https page load
```

> ⚠️ **Ban risk:** auto-select/lock is automation Riot can ban for (instalocker class).
> The read-only parts are low risk; the lock is not. Use at your own risk.

## Layout
- `pc/` — Go app → single static `aqua.exe` (`internal/{config,riot,picker,relay,ui,updater,version}`,
  `cmd/aqua`; `cmd/aquasign` is the maintainer-only release signing tool).
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

This builds the SPA (`web/dist`) and `aqua.exe`. The script also best-effort embeds the
Windows icon via `goversioninfo` (skipped cleanly if offline). To build pieces by hand:

```powershell
cd web;  bun install; bun run build           # SPA
cd pc;   go build -o ../aqua.exe ./cmd/aqua   # console app — NOT -H windowsgui
```

## Deploy the relay + SPA

```powershell
cd cloud/aqua-agent-picker-worker
bun run deploy        # publishes the Worker + web/dist, binds aqua.nguyenvu.dev
```

`wrangler` must be logged in and `nguyenvu.dev` must be a zone in your Cloudflare account.

## Cut a release (auto-update)

Releases are built and signed by `.github/workflows/release.yml`. Tag and push:

```powershell
git tag v1.1.0; git push origin v1.1.0    # or run the "release" workflow manually
```

CI builds `aqua.exe` with the version stamped in, signs it (minisign), generates
`manifest-windows-amd64.json`, and publishes all three as the GitHub Release. Clients pick it up
on their next daily check. Pass a `min_version` (manual run) to mark older clients as needing a
mandatory update — useful when a relay-protocol change makes stale binaries incompatible.

**One-time signing setup.** Generate the keypair, embed the public key, and add the secrets:

```powershell
cd pc; go run ./cmd/aquasign keygen     # prints the public key + the two secret values
```

Put the printed key in `pc/internal/version/version.go` (`PublicKey`) and add
`AQUA_SIGNING_KEY` + `AQUA_SIGNING_PASSWORD` as GitHub repo secrets. Keep the private key file
offline; it is gitignored under `dist/`.

## Run

1. Start `aqua.exe` on your PC. The console shows a **pairing QR**, the pair URL, and an
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

Run `aqua.exe -headless` to skip the console UI and log to stderr (used by the integration
tests / scripted pairing). Config lives at `%APPDATA%\Aqua\config.json`; logs at
`%APPDATA%\Aqua\aqua.log`.

## Updating

Only `aqua.exe` self-updates — the phone SPA and the relay Worker are server-side and refresh
on their own. On launch Aqua checks (at most once a day) for a newer signed release and, if one
exists, shows an **Update available** banner. To install it, quit and run:

```powershell
aqua.exe -update    # downloads, verifies signature + checksum, replaces itself in place
aqua.exe -version   # print the running version
```

The update is fetched straight from the latest GitHub Release and verified against a minisign
public key baked into the binary, so a tampered download is refused. Set `AQUA_NO_UPDATE_CHECK=1`
to silence the startup check, or `AQUA_MANIFEST_URL` to point at a staging release.

## Develop

- **UI without the PC/Worker:** `cd web && bun run mock` then open
  `http://127.0.0.1:9912/?code=DEV12345&device=devbox` — a local relay speaking the real
  protocol with a scripted game timeline.
- **Worker dev:** `cd cloud/aqua-agent-picker-worker && bun run dev` (`wrangler dev`).
- **Tests:** `go test ./...` in `pc/`; protocol suites `bun test-pairing.ts` /
  `test-picker.ts` / `test-autolock.ts` against `bun run dev`.

> Known env issue: on some Windows boxes `wrangler dev`/workerd accepts TCP but never answers
> HTTP. Use the `bun run mock` harness for UI work and a working box for the Worker.
