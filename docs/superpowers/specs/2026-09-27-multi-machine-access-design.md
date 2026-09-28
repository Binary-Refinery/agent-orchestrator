# Multi-machine access and first-class browser support — design

Status: draft for review. Not yet implemented.
Date: 2026-09-27

## 0. Goal

Every machine that runs an AO daemon can be associated with the user's AO
account, and any client the user is signed in on can control any of those
machines:

- the **desktop app** controls its own machine and the user's other machines;
- a **plain browser** does the same, with no Electron install;
- the **mobile app** keeps working and gains the same machine list.

Within that, AO gets **first-class browser support**: as much functionality as
possible works in a browser, and what genuinely cannot is greyed out with a
tooltip saying why. It is never hidden without explanation, and never shown as
standing inline error text.

## 1. Fixed product decisions

Decided by the user on 2026-09-27; this design treats them as constraints.

| Decision | Consequence for the design |
| --- | --- |
| Multi-machine control is a **paid AO Cloud feature**. | The relay runs in AO Cloud. There is no self-hosted relay. Relay bandwidth is an accepted cost, metered per account. |
| Machine sharing is **always personal**. Only cloud agents are shared across an organization. | A machine belongs to exactly one user. Machine authorization is "is this the owner", never an org role, and org admins get no access to members' machines. |
| The relay should ideally **not see traffic**, but **V1 may ship without end-to-end encryption**. | V1 terminates TLS at the relay (the same posture as today's Cloudflare quick tunnel, ADR 0004). The protocol reserves a place for E2E so it is added without a client/daemon flag day (section 6). |

## 2. What exists today

This design mostly connects existing pieces.

**Daemon, network-facing.**

- The loopback listener (`127.0.0.1:3001`) has no auth; CORS is its only browser
  boundary, and it admits every loopback origin
  (`backend/internal/httpd/cors.go`).
- A second listener on all interfaces exists only while Connect Mobile is on.
  It uses a rotating password as a `Bearer` token, per-source lockout, and a
  socket-based block on admin routes
  (`backend/internal/httpd/lan_listener.go`, ADR 0001).
- Remote mobile reach is a supervised `cloudflared` quick tunnel. Cloudflare
  terminates TLS; SSE is buffered in about 128 KB chunks and cannot stream,
  while the `/mux` WebSocket is unaffected (ADR 0004). The ADR already names two
  follow-ups: a relay with payload encryption, and carrying conversation events
  over the mux instead of SSE.

**AO Cloud.**

- WorkOS accounts, organizations, row-level-security membership, and public
  TLS ingress at `api.aoagents.dev` (`cloud/README.md`).
- Cloud workers already **dial out** to the control plane with their own JWT
  identity, and a terminal relay forwards worker frames to an attached browser
  before persisting them (`cloud/internal/workertransport/`,
  `cloud/docs/control-plane.md`). This is the same shape a machine relay needs.
- The Next.js gateway cannot proxy WebSocket upgrades today
  (`cloud/docs/control-plane.md`), so the relay cannot sit behind it.

**Renderer.**

- The renderer calls about 76 distinct methods on the Electron preload bridge
  (`window.ao`). Outside Electron, `frontend/src/renderer/lib/bridge.ts`
  substitutes stubs, several of which report "unavailable".
- `VITE_NO_ELECTRON=1` means both "no Electron" and "use mock workspaces"
  (`frontend/src/renderer/lib/preview-mode.ts`). A browser pointed at a real
  daemon sees fake data, and daemon readiness comes only from the bridge.
- The WorkOS token lives only in Electron main. The renderer reaches the Cloud
  control plane through an IPC proxy (`frontend/src/main/cloud-cp-proxy.ts`).
- The renderer assumes one daemon at one base URL.

## 3. Concepts

- **Machine**: one AO daemon installation (one data directory), owned by one
  user. It has a stable machine ID and a keypair generated on the machine; the
  private key never leaves it.
- **Client**: a desktop app, browser tab, or mobile app, signed in as the user.
- **Target**: the machine a client is currently controlling. The desktop app's
  own machine is just the target that happens to be local.
- **Capability**: whether a feature works for a given (client, target) pair.
  "Browser vs. Electron" is not enough; see section 7.

## 4. Architecture

```
 client (desktop / browser / mobile)
    │  HTTPS + WSS, WorkOS session
    ▼
 AO Cloud relay  ── machine registry (Postgres, owner-scoped)
    ▲  outbound WSS, machine key
    │
 AO daemon on machine N
```

1. **Enrollment.** The user signs in to their account on the machine: desktop
   sign-in, or `ao machine enroll` headless (device-code flow). The daemon
   generates a keypair and registers `{machineId, publicKey, name, os}` against
   the user. Cloud stores it owner-scoped. From any client, the user can see,
   rename, and revoke machines.
2. **Outbound connection.** While enrolled and allowed to accept remote
   connections, the daemon holds one outbound WebSocket to the relay,
   authenticated by signing a relay challenge with its machine key. No inbound
   port, NAT traversal, or certificate on the machine.
3. **Client connection.** A signed-in client lists the user's machines and
   opens a session to one. The relay checks that the WorkOS subject owns the
   machine, then multiplexes client streams over that machine's connection.
4. **Local requests on the daemon.** A relayed request reaches the daemon's HTTP
   handler through a third listener, the Relay Listener, not through the
   loopback listener. As with the LAN listener, the daemon applies its route
   policy based on which listener the request arrived on, so a relayed request
   can never be mistaken for a local one.
5. **Direct paths stay.** Loopback for the local desktop app, and LAN or
   Tailscale for mobile (ADR 0001/0004), remain and are preferred when they
   work. The relay replaces the Cloudflare quick tunnel as the remote path for
   subscribed users; the tunnel can stay as the free fallback or be retired.
   That is a product call, listed in section 10.

### Why a relay we run, not Cloudflare or Tailscale

- ADR 0004 already records the limits of Cloudflare quick tunnels: TLS
  terminated by a third party, SSE buffering, hostnames that rotate on restart,
  and no uptime guarantee.
- A Tailscale-based design makes the user adopt a second product.
- An AO relay ties access to the AO account, and that account is what makes this
  a paid feature. It is also the only option where we control the path to
  end-to-end encryption.

## 5. Transport

- **One connection per machine, multiplexed.** Client requests (REST),
  terminal streams, and events share the machine's relay connection as logical
  streams. The daemon already multiplexes terminals over `/mux` with topic
  subscriptions, and that is the frame format to extend.
- **Events move onto the mux.** This is ADR 0004's deferred fix. It removes the
  SSE-through-proxy problem for every remote path at once, and removes the
  mobile polling stopgaps (`packages/mobile/lib/pollInterval.ts`,
  `packages/mobile/lib/chat/conversationPoll.ts`).
  It is worth doing first because it also helps the existing tunnel.
- **The relay is a standalone service**, not part of the Next.js gateway,
  because the gateway cannot upgrade WebSockets. It can reuse the control-plane
  Go codebase and the workertransport patterns.
- **Bandwidth and metering.** The relay counts bytes per machine and account.
  Terminal output dominates, so the daemon keeps its existing bounded terminal
  frames and the relay applies per-machine backpressure rather than buffering
  without limit.

## 6. Security

**V1 (no E2E):**

- TLS client ↔ relay and daemon ↔ relay; the relay sees plaintext frames. This
  is the same posture as today's tunnel, but on infrastructure we operate and
  can state a retention policy for. The settings UI must say so plainly, as
  ADR 0004 does.
- Authorization is ownership only: the relay forwards a client stream to a
  machine only if the client's WorkOS subject is that machine's owner.
- Revoking a machine, or signing out everywhere, drops its relay connection
  and invalidates its registration.
- **Route policy is based on the action, not the client**, and is enforced on
  the daemon by listener:

  | Class | Examples | Loopback | LAN / Relay |
  | --- | --- | --- | --- |
  | Local-only | `/shutdown`, installers (`/api/v1/system/install`), dev routes, telemetry internals | yes | never |
  | Sensitive | Codex account management, Connect Mobile / relay settings | yes | with recent re-authentication (step-up) |
  | Normal | sessions, chat, terminals, files, diffs, PRs, projects | yes | yes |

  Today's mobile block list becomes the "local-only" and "sensitive" rows, so
  mobile and browser get identical, deliberate rules.

**V2 (E2E):**

- The client and daemon establish a session key (e.g. Noise or X25519 + AEAD)
  inside the relayed stream. The daemon authenticates with the machine key the
  client fetched from the registry, and the client with a per-device key bound
  to the account.
- The relay then routes and meters opaque frames only.
- **The V1 protocol reserves this now**: every relayed stream starts with a
  handshake frame whose V1 form is "no encryption", so V2 is a negotiated
  upgrade rather than a breaking change.
- Caveat to state honestly: a browser client's crypto is served by us, so
  browser E2E protects against a compromised relay, not a compromised web
  origin. Desktop and mobile clients carry their code locally.

## 7. Capabilities: what works where

The renderer replaces `window.ao ?? stubs` with an explicit **platform** built
from the (client, target) pair. Every bridge method declares one of:

- **Works through the daemon.** Implemented over HTTP/mux, so it works for any
  client and any target.
- **Works through a web API.** Clipboard, notifications, opening external links.
- **Needs the target to be the local machine.** Editor handoff, reveal in
  Finder, folder picker, the embedded Browser panel and `ao browser`: they act
  on the machine the client is sitting at.
- **Desktop shell only.** Tray, auto-update, native menus, dock badge.

The UI asks `platform.supports(capability)`. An unsupported control is
**disabled, with a tooltip that says why** ("Open in editor is only available
on the machine this session runs on", "Requires the desktop app"). It is
neither hidden without explanation nor shown as an inline error (PR #5969
establishes this pattern for the editor handoff).

Initial classification of the renderer's bridge groups (to be finalized per
method during implementation):

| Bridge group | Browser → any machine | Desktop → remote machine |
| --- | --- | --- |
| `daemon` status/readiness | via daemon (`/readyz`) | via relay |
| `uiSettings`, `appState`, `keybindings` | via daemon (new endpoints; today stored by Electron main) | via daemon |
| `telemetry` bootstrap | via daemon | via daemon |
| `clipboard` | web API | native |
| `notifications` | web API (Notification + service worker) | native |
| `cloud`, `cloudCp` (WorkOS token, CP proxy) | web sign-in with an HttpOnly session cookie; the relay/Cloud origin replaces the IPC proxy | unchanged |
| `terminal` (native helpers) | via daemon mux | via daemon mux |
| `editorHandoff` | greyed out; later a `vscode://`/`cursor://` Remote-SSH link where available | greyed out unless the target is local |
| `app` folder picker / scan import | server-side directory browser via daemon | same |
| Browser panel, `ao browser` | greyed out (V1) | greyed out unless the target is local |
| `tray`, `updates`, `updateSettings`, `window`, `featureBuilds` | greyed out / hidden chrome | local app only |

**Session app previews.** A worker's dev server binds `127.0.0.1:<port>` on its
machine (`backend/internal/previewserver/manager.go`). Remote clients reach it
through a daemon reverse proxy exposed over the relay. Path-based routing
breaks apps that use absolute URLs, so the target is per-preview subdomains on
a relay domain (`<preview>.<machine>.relay.aoagents.dev`). Until then,
previews are greyed out for remote targets.

**Browser panel on a remote or headless machine.** It needs the daemon to run
headless Chromium and stream it (CDP screencast plus input). This is a
separate project and is greyed out in V1.

## 8. Renderer changes

- **Split mock data from the platform.** Add `VITE_AO_MOCK_DATA=1` for the e2e
  harness and playground. `dev:web` then targets a real daemon by default.
- **Daemon connection object.** Replace the global `127.0.0.1:3001` base URL
  with `{machineId, transport: loopback | lan | relay, baseUrl, auth}`. React
  Query keys and the mux subscription are scoped by machine, so switching
  machines never mixes caches.
- **Machine switcher** in the sidebar, grouping projects by machine, with online
  and offline state from the relay.
- **The daemon serves the built renderer** on its listeners, so
  `http://<machine>/` or the relay URL opens the app same-origin, with no Vite
  or CORS setup.
- **Browser sign-in** reuses the Cloud UI's WorkOS web flow. The token stays in
  an HttpOnly cookie on the relay/Cloud origin, preserving today's property
  that the renderer never holds the bearer token.

## 9. Phases

Each phase ships independently and is useful without the next.

1. **Browser platform layer.**
   - The (client, target) capability model, and every bridge method classified.
   - Unsupported controls greyed out with tooltips.
   - Mock data split from the platform flag; daemon readiness checked over HTTP.

   Outcome: a browser on the same machine is a supported client.
2. **Daemon-backed state and serving.** Endpoints for `uiSettings`, `appState`,
   and `keybindings`, the daemon serving the renderer, and a server-side
   directory browser. Outcome: a browser has full local parity except for
   features that need the local machine.
3. **Events over the mux** (ADR 0004 follow-up). Fixes chat streaming over any
   proxy and removes the mobile polling stopgaps.
4. **Route policy based on action, plus a single remote-access setting.** The
   table in section 6 enforced per listener, and "Connect Mobile" generalized to
   "Remote access" in the UI.
5. **Machine registry, enrollment, and the relay (V1, no E2E).**
   - Owner-scoped registry, desktop and headless enrollment.
   - The relay service with metering, and the Relay Listener on the daemon.
   - The machine switcher, browser sign-in, and the paid-feature entitlement check.
6. **Preview proxy** over the relay with per-preview subdomains.
7. **E2E encryption (V2)** through the reserved handshake.
8. **Headless Browser panel** for remote and headless machines.

Phases 1–4 need no Cloud work and also improve today's LAN and tunnel
experience.

## 10. Open questions

1. **Free tier fallback.** Once the relay ships, does the Cloudflare quick
   tunnel stay as a free, degraded remote path, or is remote access off the
   local network paid-only?
2. **Offline behaviour.** Do clients cache the machine list and last-known
   session state for offline machines, or show them as unavailable only?
3. **Headless servers.** Is `ao machine enroll` (device code) enough, or do we
   need non-interactive enrollment tokens for provisioning fleets of
   build/dev boxes?
4. **Step-up re-authentication.** For "sensitive" routes over the relay: WorkOS
   re-authentication, or a per-machine PIN confirmed on the machine?
5. **Data residency.** The relay region(s), and whether the relay persists
   anything beyond metering. Proposed: nothing, since only cloud agents have
   durable Cloud-side state.

## 11. Non-goals

- Sharing personal machines with organization members, now or later (section 1).
- Self-hosting the relay.
- Replacing loopback/LAN/Tailscale direct paths; the relay is additive.
- Streaming a remote machine's native desktop apps (editors, Finder).
