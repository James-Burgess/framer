# Plan: local-only photo frame app

Replaces Frameo on the **Rayswitch RSFM101** (Rockchip RK3126, Android 6.0.1)
with a self-hosted, cloud-free photo frame. Gift for dad; the frame lives at
dad's house.

## Goals

- Dad watches a slideshow of photos. Zero interaction, no accounts, nothing to
  maintain.
- Photos live **only** on the frame's local storage (28 GB on `/data`). They
  never touch a third-party server.
- Family can add photos from dad's home network via a friendly local address.
- The owner can also add/manage photos **remotely** from their own house,
  without any public cloud or port-forwarding.

## Architecture

The frame runs two things:

1. **Kiosk** — a fullscreen Android WebView app (the always-on display) that
   loads the slideshow from `http://localhost:<port>`.
2. **Local server** — a single lightweight process that:
   - serves the slideshow page,
   - serves a photo **upload/manage** page,
   - stores photos on local disk,
   - (optionally) serves thumbnails / a small API.

Both live on the frame. No separate always-on device required.

```
                 ┌───────────────────────────────────────────┐
 dad's LAN       │  frame (RK3126, Android 6.0.1)            │
                 │                                            │
  family ───────►│  frame.local ──► local server              │
  (mDNS)         │                    ├─ slideshow (kiosk)    │
                 │                    ├─ upload page          │
  owner ────────►│  frame.<tailnet> ──┘   └─ /data/photos     │
  (Tailscale)    └───────────────────────────────────────────┘
```

## Networking

### Local: `frame.local`

`frame.local` resolves via mDNS (Bonjour/ZeroConf) — no router or DNS setup.

- Set the device hostname to `frame` and persist it across reboots.
- Register an mDNS service from the app so the name is advertised reliably.
- **Fallback:** assign a static IP / DHCP reservation in dad's router so the
  family has a stable address if mDNS flakes on some client.

Supported natively by macOS, iOS, Android, Windows 10+, and modern Linux.

### Remote: Tailscale

Tailscale gives the owner an encrypted path to the frame from anywhere, with no
public cloud and no port-forwarding.

- Adds a `tailscale0` interface with a `100.x` CGNAT address.
- **Split-tunnel by default** — only `100.x` traffic uses the mesh; local LAN
  traffic (and `frame.local`) is unaffected. This is what we want.
- **Do not** enable "exit node" mode (it would tunnel all frame traffic
  elsewhere).
- The owner reaches the frame via `frame.<tailnet>.ts.net` (or the `100.x` IP);
  dad's family keeps using `frame.local`. Both paths hit the same app.

#### Caveats

- Tailscale's Android app requires Android 6.0+; the frame is exactly 6.0.1,
  so it *should* work but is at the version floor — needs a test.
- Tailscale is a GUI app with a one-time login (QR / auth URL). Plan: install
  the APK, tap through login once on the frame's touchscreen, then it runs as a
  background VPN service. Expose a hidden launcher entry in our own app to
  reach it again if needed.

## Auth

Auth here is only "who may push photos to dad's frame" — not a full identity
system. Chosen model: **shared family token**, layered on **Tailscale
identity** for remote access.

- **Local LAN:** the upload page requires a single shared token (one secret the
  family knows; can be baked into a bookmark). Keeps casual Wi-Fi guests out.
- **Remote:** Tailscale membership already restricts who can reach the frame at
  all; the same token is a cheap second layer.

Deliberately **not** doing: per-person accounts, invite codes, OAuth — overkill
for a frame.

## App design

- **Stack:** a single lightweight server (Go or Node) serving the slideshow and
  a simple upload API, storing photos under `/data/photos` (SQLite or plain
  files + a JSON index for metadata).
- **Kiosk:** a minimal Android WebView app (`MainActivity` as Home) that loads
  `http://localhost:<port>` fullscreen, no chrome, auto-start on boot.
- **Features (v1):**
  - Slideshow (shuffle/order, per-photo duration, auto-rotate, screen-on).
  - Upload page (`POST` multipart) gated by the shared token.
  - `frame.local` + mDNS registration.
  - Keep-awake so the display doesn't sleep.
- **Later:** remote-friendly management (album management, delete/rotate,
  captions), Tailscale integration, maybe a QR code on the frame showing the
  local URL.

## Implementation order

1. **Core (do first — this is the gift):**
   - Scaffold the local server (slideshow + upload page + token).
   - Build the Android WebView kiosk; set it as the Home app.
   - Set hostname `frame` + mDNS; verify `frame.local` resolves on the LAN.
   - Disable `net.frameo.frame` (now safe — we have a replacement Home).
2. **Remote (after core works):**
   - Install Tailscale, log in once, verify `frame.<tailnet>.ts.net`.
   - Confirm local `frame.local` still works alongside (split tunnel).
3. **Harden:**
   - Verify no outbound traffic after Frameo is disabled.
   - Optionally block leftover domains at the router/DNS level.
4. **Document:**
   - Finalize exact setup steps for repeatability (new frames / re-flash).

## Prerequisites already done

- ✅ ADB + root (SuperSU `su`), `persist.adb.tcp.port=5555`.
- ✅ Full backup (`backups/` — boot chain + `/system`, hashed).
- ✅ Debloat — 9 vendor apps removed from `/system` (Adups FOTA, Rockchip OTA,
  factory tests, Rk* junk). Frameo still active pending replacement.

## Open questions

- [x] Preferred server language — **Go** (`CGO_ENABLED=0`, static `linux/arm` GOARM=7 binary bundled inside the APK as `lib/armeabi-v7a/libfrmaeo.so`).
- [x] Photo organization — **Hybrid**: local Family photos (with optional Albums & Favorites) + pull-only curated Public categories & image search (cached locally).
- [x] `frame.local` mDNS — solved via built-in pure-Go mDNS responder + Android `WifiManager.MulticastLock` (reboot-proof).
- [ ] Whether a second always-on device at dad's house is acceptable if
      Tailscale-on-Android-6.0.1 proves unreliable (fallback: subnet router).
