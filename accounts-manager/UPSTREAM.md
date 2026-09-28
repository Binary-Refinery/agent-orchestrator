# Accounts Manager upstream engine

The source under `engine/` is a pinned snapshot of CLIProxyAPI with the callback
extension listed below.

- Upstream: https://github.com/router-for-me/CLIProxyAPI
- Tag: `v7.3.8`
- Commit: `c93978c4ea2e908255a2a06c37599fda3651554a`
- Imported: 2026-09-19
- License: MIT; see `engine/LICENSE`

AO-specific lifecycle, API, storage, and product integration should live outside
`engine/`. Keeping the snapshot isolated makes upstream updates reviewable and
prevents AO-specific behavior from being mixed into the provider engine.

When updating the snapshot, replace `engine/` from a clean upstream checkout,
excluding only its `.git` directory, and update the tag and commit above in the
same change.

## Local callback extension

`sdk/auth/LoginOptions` accepts an owned loopback callback listener and a private
authorization-URL delivery function. The two existing browser authenticators use
the in-memory callback receiver when supplied; their default flows and provider
exchange implementation remain available. Callback state and redirect binding
are checked before delivery, errors omit callback values, and cancellation closes
the listener. Callback-related logging no longer prints authorization codes or
state values.

The extension and boundary tests live in `sdk/auth/listener_callback*.go`; the
only existing SDK files changed are `interfaces.go` and the two browser
authenticator entry points. Preserve or reapply this surface when updating the
snapshot. Credential persistence and lifecycle integration stay in the runner.
