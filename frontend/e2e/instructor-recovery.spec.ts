import { expect, test, type Page } from "@playwright/test";
import { installFakeAgent } from "./support/fake-bridge";

// Real renderer with deterministic daemon responses. Upgrade process ownership
// and persistent policy admission are covered by the backend upgrade tests.
async function fixture(page: Page, needsRecovery = false) {
	await installFakeAgent(page, { projectId: "recovery-project", projectName: "Recovery project" });
	const at = "2026-10-13T09:00:00Z";
	let revision = 1, reads = 0, recovered = !needsRecovery;
	const requests: Array<{ path: string; body: Record<string, unknown> }> = [];
	const policy = { schema: 1, version: "policy-old", initialPhase: "implementation", roles: { orchestrator: { harness: "codex", model: "native-model", effort: "configured", effortByModel: { "native-model": "native-max" } } }, profiles: {}, transitions: [], closeoutRequires: ["review", "delta"], maxFixRounds: 2 };
	const target = { ...policy, version: "policy-new", roles: { ...policy.roles, worker: { harness: "opencode", model: "worker-native", effort: "maximum", effortByModel: { "worker-native": "native-high" } } } };
	const work = () => ({ id: "work-recover", projectId: "recovery-project", name: "Preserve existing login work", goal: "Continue the existing reviewed work", phase: "implementation", state: "waiting_human", next: "Review the retained gate", policyVersion: "policy-old", policy, revision, evidence: [], actions: recovered ? ["go"] : [], gate: { id: "gate-retained", kind: "review", title: "Retained review approval", exactApproval: "GO reviewed-login" }, progressAt: at });
	const recovery = () => ({ workId: "work-recover", workRevision: revision, policyVersion: "policy-old", sessionId: "recovery-project-1", sessionCreatedAt: at, providerConversationId: "retained-native-thread", controllerGeneration: "generation-4", ready: recovered, action: recovered ? "none" : "resume", reasonCode: recovered ? "READY" : "CONTROLLER_UNAVAILABLE", reason: "The existing controller needs to be reconnected." });
	const commands = [{ action: "go", name: "Go", mutates: true, snapshotRequired: true }];
	await page.route("http://127.0.0.1:8080/api/v1/projects/*", (route) => route.fulfill({ json: { status: "ok", project: { config: { orchestrator: { agent: "codex", agentConfig: { model: "native-model", effort: "native-max" } } } } } }));
	await page.route("http://127.0.0.1:8080/api/v1/instructor/**", async (route) => {
		const request = route.request(), path = new URL(request.url()).pathname;
		if (request.method() === "POST") requests.push({ path, body: request.postDataJSON() });
		if (path.endsWith("/capabilities")) return route.fulfill({ json: { enabled: true, commands, publication: { available: true } } });
		if (path.endsWith("/status")) { reads++; return route.fulfill({ json: { id: `snapshot-${revision}`, works: [work()], createdAt: at, commands, recoveries: { "work-recover": recovery() } } }); }
		if (path.endsWith("/inventory")) return route.fulfill({ json: { capturedAt: at, projects: [{ id: "recovery-project", name: "Recovery project" }], sessions: [], resources: [] } });
		if (path.endsWith("/projects/recovery-project/policy")) return route.fulfill({ json: { policy: { ...target, version: revision === 1 ? "policy-new" : "policy-newer" }, source: "project:recovery-project" } });
		if (path.endsWith("/policy")) return route.fulfill({ json: policy });
		if (path.endsWith("/policy-rebind/preview")) return route.fulfill({ json: { workId: "work-recover", snapshotId: "snapshot-1", workRevision: 1, beforePolicyVersion: "policy-old", targetPolicyVersion: "policy-new", policySource: "project:recovery-project", beforePolicy: policy, effectivePolicy: target, proposalHash: "target-bound-hash", exactApproval: "REBIND-GO work-recover target-bound-hash", invalidatedTransitions: ["review-approved"], supersededExceptionIds: [], historicalEvidenceCount: 3 } });
		if (path.endsWith("/policy-rebind")) return route.fulfill({ json: work() });
		if (path.endsWith("/recovery")) { if (request.method() === "POST") recovered = true; return route.fulfill({ json: recovery() }); }
		if (path.endsWith("/works/work-recover")) return route.fulfill({ json: { work: work(), dispatches: [], events: [] } });
		if (path.endsWith("/commands")) return route.fulfill({ json: { dispatches: [], skipped: [] } });
		return route.fulfill({ json: { jobs: [] } });
	});
	return { requests, change: () => { revision++; }, reads: () => reads, markRecovered: () => { recovered = true; } };
}

test("renderer: missing controller recovers the same conversation before another approval @T0 @INSTRUCTOR_RECOVERY", async ({ page }) => {
	const state = await fixture(page, true);
	await page.goto("/#/instructor-board");
	const card = page.getByRole("article", { name: "Preserve existing login work", exact: true });
	await expect(card.getByRole("button", { name: "Review gate", exact: true })).toHaveCount(0);
	await card.getByRole("button", { name: "Recover session", exact: true }).click();
	const dialog = page.getByRole("dialog", { name: "Recover session", exact: true });
	await expect(dialog.getByText(/recovery-project-1/)).toBeVisible();
	expect(state.requests).toHaveLength(0);
	await dialog.getByRole("button", { name: "Reconnect existing conversation", exact: true }).click();
	await expect(dialog.getByRole("status")).toContainText("The controller is connected");
	expect(state.requests).toEqual([{ path: "/api/v1/instructor/works/work-recover/recovery", body: { workId: "work-recover", workRevision: 1, policyVersion: "policy-old", sessionId: "recovery-project-1", sessionCreatedAt: "2026-10-13T09:00:00Z", providerConversationId: "retained-native-thread", controllerGeneration: "generation-4" } }]);
	await dialog.getByRole("button", { name: "Return to work", exact: true }).click();
	await expect(card.getByRole("button", { name: "Review gate", exact: true })).toBeVisible();
	expect(state.requests.some((request) => request.path.endsWith("/commands"))).toBe(false);
});

test("renderer: policy rebind approval keeps its captured revision and target during polling @T0 @INSTRUCTOR_RECOVERY", async ({ page }) => {
	const state = await fixture(page); await page.clock.install(); await page.goto("/#/instructor-board");
	await page.getByRole("button", { name: "Preserve existing login work", exact: true }).click();
	await page.getByRole("button", { name: "Change work configuration", exact: true }).click();
	const dialog = page.getByRole("dialog", { name: "Change work configuration", exact: true });
	await dialog.getByRole("textbox", { name: "Reason for changing this work" }).fill("Adopt reviewed project configuration.");
	await dialog.getByRole("button", { name: "Preview configuration change" }).click();
	await expect(dialog.getByText("policy-old → policy-new", { exact: true })).toBeVisible();
	await dialog.getByRole("textbox", { name: "Enter the exact approval shown above" }).fill("REBIND-GO work-recover target-bound-hash");
	state.change(); await page.clock.fastForward(16_000); await expect.poll(state.reads).toBeGreaterThanOrEqual(2);
	await expect(dialog.getByRole("textbox", { name: "Reason for changing this work" })).toHaveValue("Adopt reviewed project configuration.");
	await expect(dialog.getByRole("textbox", { name: "Enter the exact approval shown above" })).toHaveValue("REBIND-GO work-recover target-bound-hash");
	await expect(dialog.getByText("policy-old → policy-new", { exact: true })).toBeVisible();
	await dialog.getByRole("button", { name: "Approve configuration change" }).click();
	await expect.poll(() => state.requests.length).toBe(2);
	expect(state.requests[1]).toEqual({ path: "/api/v1/instructor/policy-rebind", body: { workId: "work-recover", snapshotId: "snapshot-1", reason: "Adopt reviewed project configuration.", targetPolicyVersion: "policy-new", approval: "REBIND-GO work-recover target-bound-hash" } });
});


test("renderer: a native recovery rejection exposes corrective settings and its request id @T0 @INSTRUCTOR_RECOVERY", async ({ page }) => {
	const state = await fixture(page, true);
	await page.route("http://127.0.0.1:8080/api/v1/instructor/works/work-recover/recovery", async (route) => {
		if (route.request().method() !== "POST") {
			if (state.requests.length) { await route.fulfill({ json: { ...state.requests[0].body, ready: false, action: "resume", reasonCode: "INSTRUCTOR_RESUME_BLOCKED", reason: "Role requires harness opencode, got codex." } }); return; }
			await route.fallback(); return;
		}
		state.requests.push({ path: new URL(route.request().url()).pathname, body: route.request().postDataJSON() });
		if (state.requests.length > 1) { state.markRecovered(); await route.fulfill({ json: { ...route.request().postDataJSON(), ready: true, action: "none", reasonCode: "READY", reason: "Controller connected" } }); return; }
		await route.fulfill({ status: 409, json: { error: "conflict", code: "INSTRUCTOR_RESUME_BLOCKED", message: "Role requires harness opencode, got codex. Review the pinned role policy.", requestId: "recovery-request-17", details: { cause: "role requires harness opencode, got codex" } } });
	});
	await page.goto("/#/instructor-board");
	await page.getByRole("button", { name: "Recover session", exact: true }).click();
	const dialog = page.getByRole("dialog", { name: "Recover session", exact: true });
	await dialog.getByRole("button", { name: "Reconnect existing conversation", exact: true }).click();
	await expect(dialog.getByRole("alert")).toContainText("Role requires harness opencode, got codex");
	await expect(dialog.getByRole("alert")).toContainText("INSTRUCTOR_RESUME_BLOCKED");
	await expect(dialog.getByRole("alert")).toContainText("recovery-request-17");
	await expect(dialog.getByRole("button", { name: "Reconnect existing conversation", exact: true })).toBeDisabled();
	await expect(dialog.getByRole("button", { name: "Open project settings", exact: true })).toBeEnabled();
	expect(state.requests).toHaveLength(1);
	// The fixed configuration does not clear the old probe reason. Rechecking
	// must release the local retry guard without issuing a recovery mutation.
	await dialog.getByRole("button", { name: "Check again", exact: true }).click();
	await expect(dialog.getByRole("button", { name: "Reconnect existing conversation", exact: true })).toBeEnabled();
	await expect(dialog.getByRole("button", { name: "Open project settings", exact: true })).toBeEnabled();
	expect(state.requests).toHaveLength(1);
	await dialog.getByRole("button", { name: "Reconnect existing conversation", exact: true }).click();
	await expect(dialog.getByRole("status")).toContainText("The controller is connected");
	expect(state.requests).toHaveLength(2);
	expect(state.requests.some((request) => request.path.endsWith("/commands"))).toBe(false);
});
