/**
 * Update state vocabulary shared by the main process and the renderer.
 *
 * Deliberately in `shared/` and free of Node imports. `main/update-settings`
 * reads and writes the settings file, so its first line is `node:fs/promises`;
 * a renderer module that imports a VALUE from there (rather than only a type)
 * drags Node built-ins into the browser bundle and the renderer fails to boot.
 * Types were safe because they erase at compile time. These constants and
 * predicates are not, so they live here and are re-exported from there.
 */

// Live state of an automatic or manual update check/download, streamed to the
// renderer so Settings and the sidebar can reflect progress.
// "retry-scheduled" is a calm, non-error resting state: a staged build failed to
// install but AO will fetch and prepare it again on a later check, so the
// renderer shows a plain "will try again" line, not a red failure.
export type UpdateState =
	"idle" | "checking" | "available" | "not-available" | "downloading" | "preparing" | "downloaded" | "retry-scheduled" | "error" | "unsupported";

/**
 * What each state means to a consumer, so guards ask a question instead of
 * listing state names.
 *
 * Every guard that used to spell out its own subset ("is it error?", "should the
 * poll keep running?") silently excluded `retry-scheduled` the moment it was
 * added, because a missing name reads as a legitimate `false`. Declaring the
 * mapping as a total Record means adding a state fails the build here until it
 * is classified, which is the only thing that stops the next one slipping past.
 *
 * `progress`: an operation is running and will produce another status.
 * `failure`:  a failure already reported to the user; do not overwrite it.
 * `resting`:  a settled, non-failure status.
 */
export const UPDATE_STATE_KIND = {
	idle: "resting",
	checking: "progress",
	available: "resting",
	"not-available": "resting",
	downloading: "progress",
	preparing: "progress",
	downloaded: "resting",
	"retry-scheduled": "failure",
	error: "failure",
	unsupported: "resting",
} as const satisfies Record<UpdateState, "progress" | "resting" | "failure">;

/** A failure already on screen. A later generic broadcast must not replace it. */
export function isReportedFailure(state: UpdateState): boolean {
	return UPDATE_STATE_KIND[state] === "failure";
}

/** An updater operation is still running and will report again. */
export function isUpdateInProgress(state: UpdateState): boolean {
	return UPDATE_STATE_KIND[state] === "progress";
}

/**
 * Whether the renderer's background reconcile poll is worth running in a state.
 *
 * A separate axis from the kind above: `available` and `downloaded` are settled
 * yet still worth re-reading, while `not-available` is settled and inert. Written
 * as a total Record for the same reason, so a new state cannot default to "no
 * polling" just by being forgotten.
 */
export const UPDATE_STATE_RECONCILES = {
	idle: false,
	checking: true,
	available: true,
	"not-available": false,
	downloading: true,
	preparing: true,
	downloaded: true,
	"retry-scheduled": true,
	error: true,
	unsupported: false,
} as const satisfies Record<UpdateState, boolean>;

export function shouldReconcileUpdateStatus(state: UpdateState): boolean {
	return UPDATE_STATE_RECONCILES[state];
}

/**
 * Whether a status ends a pending channel switch.
 *
 * NOT the same as "not in progress": `available` and `downloaded` are settled but
 * mean the switch found something to install, so the "update and restart to
 * switch" prompt has to stay up. Only outcomes that leave nothing to install, or
 * that AO is still working through by itself, retire it.
 */
export const UPDATE_STATE_RESOLVES_CHANNEL_SWITCH = {
	idle: false,
	checking: false,
	available: false,
	"not-available": true,
	downloading: false,
	preparing: false,
	downloaded: false,
	"retry-scheduled": true,
	error: true,
	unsupported: true,
} as const satisfies Record<UpdateState, boolean>;

export function resolvesChannelSwitch(state: UpdateState): boolean {
	return UPDATE_STATE_RESOLVES_CHANNEL_SWITCH[state];
}

/**
 * How long the main process waits for the feed before giving up on a check.
 *
 * Shared because the renderer runs its own watchdog over the same call. When the
 * renderer's ceiling was the shorter of the two it declared failure while a slow
 * check was still legitimately running, then had no way to take it back once the
 * check succeeded. Any renderer deadline must be derived from this, never picked
 * independently.
 */
export const UPDATE_CHECK_TIMEOUT_MS = 180_000;
