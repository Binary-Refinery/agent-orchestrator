import { expect, test, type Page } from "@playwright/test";
import { agentReadiness } from "../src/renderer/test/agent-readiness-fixtures";
import { installFakeAgent } from "./support/fake-bridge";

// Renderer smoke only: the bridge and daemon HTTP responses are fixtures. These
// cases exercise the real renderer/API client, not daemon persistence, delivery,
// evidence verification or provider acceptance. Core integration tests own those.

const at = "2026-10-11T10:00:00Z";
const registry = [
	{ name: "Status", aliases: ["status"], action: "status", description: "Full frozen work snapshot", mutates: false, snapshotRequired: false },
	{ name: "Tore", aliases: ["tore"], action: "tore", description: "Open Human decisions", mutates: false, snapshotRequired: false },
	{ name: "Status Delta", aliases: ["delta"], action: "delta", description: "Changes since the displayed snapshot", mutates: false, snapshotRequired: true },
	{ name: "Weiter", aliases: ["weiter", "weiter alle"], action: "weiter", description: "Continue authorized work", mutates: true, snapshotRequired: true },
	{ name: "Go", aliases: ["go", "go all"], action: "go", description: "Approve the displayed gate", mutates: true, snapshotRequired: true },
	{ name: "Details", aliases: ["details"], action: "details", description: "Evidence and history", mutates: false, snapshotRequired: false },
	{ name: "Prüfen", aliases: ["pruefen"], action: "pruefen", description: "Ask the orchestrator to verify", mutates: true, snapshotRequired: true },
	{ name: "Stopp", aliases: ["stopp"], action: "stopp", description: "Pause planned steps", mutates: true, snapshotRequired: true },
	{ name: "Neu", aliases: ["neu"], action: "neu", description: "Register work", mutates: true, snapshotRequired: false },
].map((spec) => ({ ...spec, parameters: spec.action === "neu" ? [{ name: "new", type: "work-registration", required: true }] : spec.snapshotRequired ? [{ name: "snapshotId", type: "snapshot-reference", required: true }] : [], scopes: spec.action === "neu" ? ["project"] : ["portfolio", "work"], authority: spec.mutates ? "operator" : "reader", preconditions: spec.mutates ? ["current-work-revision", "action-eligible"] : [] }));
const policy = {
	schema: 1, version: "policy-renderer-v1", initialPhase: "intake",
	roles: {
		instructor: { harness: "opencode", model: "provider/model-a", effort: "maximum", effortByModel: { "provider/model-a": "native-high" } },
		orchestrator: { harness: "opencode", model: "provider/model-a", effort: "maximum", effortByModel: { "provider/model-a": "native-high" } },
		worker: { harness: "opencode", model: "provider/model-a", effort: "maximum", effortByModel: { "provider/model-a": "native-high" } },
		reviewer: { harness: "codex", model: "codex/model-a", effort: "maximum", effortByModel: { "codex/model-a": "xhigh" }, independent: true },
	},
	profiles: { push: { command: ["npm", "run", "check:push"], timeoutSeconds: 60 }, full: { command: ["npm", "test"], timeoutSeconds: 120 } },
	transitions: [{ from: "review", to: "triage", role: "reviewer", requires: ["review"], humanGate: true }],
	closeoutRequires: ["push", "full", "review", "delta"], maxFixRounds: 2, nudgeAfterSeconds: 600, escalateAfterSeconds: 1200,
	attribution: { authorName: "Operator", authorEmail: "operator@example.invalid", coAuthorName: "Agent", coAuthorEmail: "agent@example.invalid", sessionTrailer: "AO-Session", issueTrailer: "Issue" },
	traceability: { required: true, requireAssignee: true, projectNumber: 0, requiredPhases: ["review"], allowedRepositories: ["owner/repo"], allowedBaseBranches: ["main"] },
};

function work(id: string, name: string, patch: Record<string, unknown> = {}) {
	return { id, name, projectId: "instructor-demo", goal: `${name} goal`, source: "issue", sourceRef: `#${id}`, sessionIds: [], launches: [], state: "ready", phase: "implementation", phaseSequence: 1, fixPhaseSequence: 0, deltaPhaseSequence: 0, phaseStartedAt: at, summary: "Requirements accepted", next: "Implement the change", policyVersion: policy.version, policy, evidence: [], fixRounds: 0, openCriticalFindings: 0, revision: 1, createdAt: at, updatedAt: at, progressAt: at, actions: ["details", "weiter", "pruefen", "stopp"], ...patch };
}

function snapshot(id = "snapshot-renderer-v1") {
	return { id, createdAt: at, commands: registry, commandConflicts: [], metrics: { closedLast7Days: 7, gatesClearedLast24Hours: 13, active: 1, gated: 1, stalled: 0, parked: 1, recordedViolations: 0, verificationFailures: 0 }, works: [
		work("work-login", "Login flow", { launches: [{ role: "worker", sessionId: "worker-login", requested: { harness: "opencode", model: "provider/model-a", effort: "native-ultra" }, sent: { harness: "opencode", model: "provider/model-a", effort: "native-high" }, status: "spawned", launchedAt: at }] }),
		work("work-publish", "Publish upgrade", { state: "waiting_human", phase: "publication", branch: "feature/upgrade", actions: ["details", "go", "stopp"], gate: { id: "gate-publication", kind: "publication", title: "Publish reviewed upgrade", exactApproval: "PUBLISH-GO feature/upgrade", openedAt: at } }),
		work("work-old", "Closed cleanup", { state: "done", phase: "closeout", actions: ["details"], summary: "Closeout recorded" }),
		work("work-parked", "Deferred migration", { state: "parked", phase: "intake", actions: ["details", "stopp"], waitCondition: "Wait for database window", waitUntil: "2026-10-12T12:00:00Z" }),
	] };
}

async function fixture(page: Page, enabled = true, uncertain = false, publication = false) {
	await installFakeAgent(page, { projectId: "instructor-demo", projectName: "Instructor demo" });
	const commands: Array<Record<string, unknown>> = [];
	const planRequests: Array<Record<string, unknown>> = [];
	const planJobs: Array<Record<string, unknown>> = [];
	const savedPolicies: Array<Record<string, unknown>> = [];
	const reconciliations: Array<Record<string, unknown>> = [];
	const previews: Array<Record<string, unknown>> = [];
	const exceptions: Array<Record<string, unknown>> = [];
	let currentPolicy = structuredClone(policy);
	let statusReads = 0;
	let restoredGate = publication;
	function currentSnapshot(id?: string) {
		const result = snapshot(id);
		if (uncertain || restoredGate) result.works[0] = work("work-login", "Login flow", { state: uncertain ? "blocked" : "waiting_human", phase: "publication", revision: uncertain ? 1 : 2, actions: uncertain ? ["details", "stopp"] : ["details", "go", "stopp"], gate: uncertain ? { id: "delivery-exact", kind: "delivery_uncertain", title: "Native delivery uncertain", dispatchId: "dispatch-exact", targetSessionId: "orch-exact", openedAt: at } : { id: "held-publish", kind: "publication", title: "Publish login", exactApproval: "PUBLISH-GO feature/login", openedAt: at }, ...(uncertain ? { suspendedGate: { id: "held-publish", kind: "publication", title: "Publish login", exactApproval: "PUBLISH-GO feature/login", openedAt: at } } : {}), verification: { key: "source-phase", status: "waiting", passes: 2, maxPasses: 2, claims: 3, verified: 1, lastRunAt: at, nextRunAt: "0001-01-01T00:00:00Z" } });
		return result;
	}
	await page.route("http://127.0.0.1:8080/api/v1/**", async (route) => {
		const request = route.request();
		const pathname = new URL(request.url()).pathname;
		if (pathname === "/api/v1/instructor/capabilities") { await route.fulfill({ json: { schema: "ao.instructor.v1", enabled, reason: enabled ? undefined : "Enable AO_INSTRUCTOR in daemon configuration", commands: registry } }); return; }
		if (pathname === "/api/v1/instructor/plans") {
			if (request.method() === "POST") {
				const input = request.postDataJSON(); planRequests.push(input);
				const job = { id: "plan-renderer", requestHash: "request-hash", input, policyVersion: policy.version, requested: { harness: "opencode", model: "provider/model-a", effort: "native-high" }, sent: { harness: "opencode", model: "provider/model-a", effort: "native-high" }, confirmed: { harness: "opencode", model: "provider/model-a", effort: "" }, state: "completed", output: { summary: "Ready work can continue; publication needs the operator.", questions: ["Which release should ship?"], proposals: [{ command: "go", workIds: ["work-publish"], reason: "Review release publication gate" }], intake: [{ projectId: "instructor-demo", name: "Investigate retry", goal: "Find retry root cause", issueId: "456" }, { projectId: "instructor-demo", name: "Improve retry visibility", goal: "Show retry outcomes", issueId: "457" }] }, createdAt: at, updatedAt: at };
				planJobs.push(job); await route.fulfill({ status: 202, json: { ...job, state: "queued", output: undefined, sent: undefined, confirmed: undefined } }); return;
			}
			await route.fulfill({ json: { jobs: planJobs } }); return;
		}
		if (pathname === "/api/v1/instructor/status") { statusReads++; await route.fulfill({ json: currentSnapshot() }); return; }
		if (pathname === "/api/v1/instructor/reconcile") { reconciliations.push(request.postDataJSON()); uncertain = false; restoredGate = true; await route.fulfill({ json: currentSnapshot().works[0] }); return; }
		if (pathname === "/api/v1/instructor/exceptions/preview") { const body = request.postDataJSON(); previews.push(body); await route.fulfill({ json: { workId: body.workId, policyVersion: policy.version, proposalHash: "reviewed-proposal", exactApproval: "EXCEPTION-GO work-login reviewed-proposal", effectivePolicy: { ...policy, maxFixRounds: body.maxFixRounds ?? policy.maxFixRounds } } }); return; }
		if (pathname === "/api/v1/instructor/exceptions") { exceptions.push(request.postDataJSON()); await route.fulfill({ json: currentSnapshot().works[0] }); return; }
		if (pathname === "/api/v1/instructor/inventory") {
			await route.fulfill({ json: { capturedAt: at, atomic: false, enabled: true, cached: true, scanError: "", projects: [{ id: "instructor-demo", name: "Instructor demo", path: "/demo", kind: "repository" }], sessions: [{ id: "session-orphan", projectId: "instructor-demo", kind: "worker", harness: "codex", mode: "tui", displayName: "Unassigned review", activityState: "idle", isTerminated: false, branch: "review/orphan", workIds: [], assignment: "unassigned", createdAt: at, updatedAt: at }], resources: [{ id: "branch-orphan", kind: "branch", projectId: "instructor-demo", repositoryPath: "/demo", path: "/demo", branch: "feature/orphan", dirty: false, status: "discovered", sessionIds: [], workIds: [], assignment: "unassigned", firstSeenAt: at, lastSeenAt: at }, { id: "probe-failed", kind: "repository", projectId: "instructor-demo", repositoryPath: "/missing", path: "/missing", dirty: false, status: "probe_failed", sessionIds: [], workIds: [], assignment: "unknown", error: "Repository unavailable", firstSeenAt: at, lastSeenAt: at }], unassignedCount: 1, unassignedResourceCount: 2 } }); return;
		}
		if (pathname.startsWith("/api/v1/instructor/works/")) {
			const id = pathname.split("/").at(-1);
			const item = currentSnapshot().works.find((candidate) => candidate.id === id);
			await route.fulfill({ json: { work: item, events: [{ id: "event-1", workId: id, kind: "registered", detail: "Operator intake", at }], dispatches: [] } }); return;
		}
		if (pathname === "/api/v1/instructor/commands" && request.method() === "POST") {
			const body = request.postDataJSON() as Record<string, unknown>;
			commands.push(body);
			if (body.command === "status" || body.command === "delta" || body.command === "tore") { await route.fulfill({ json: { snapshot: currentSnapshot(body.command === "delta" ? "snapshot-delta-v2" : "snapshot-renderer-v2"), dispatches: [], skipped: [] } }); return; }
			await route.fulfill({ json: { batchId: "batch-renderer", dispatches: [{ id: `dispatch-${commands.length}`, batchId: "batch-renderer", workId: "work-login", projectId: "instructor-demo", snapshotId: body.snapshotId, workRevision: 1, position: 0, action: body.command, message: "Fixture dispatch", state: "pending", attempts: 0, createdAt: at, updatedAt: at }], skipped: body.command === "go all" ? ["work-publish: exact publication approval required"] : [] } }); return;
		}
		if (pathname === "/api/v1/instructor/policy") {
			if (request.method() === "PUT") { const body = request.postDataJSON() as typeof policy; savedPolicies.push(body); currentPolicy = { ...body, version: "policy-renderer-v2" }; }
			await route.fulfill({ json: currentPolicy }); return;
		}
		if (pathname === "/api/v1/agents/readiness" || pathname === "/api/v1/agents/readiness/ensure") { await route.fulfill({ json: { agents: [agentReadiness("opencode", "OpenCode"), agentReadiness("codex", "Codex")] } }); return; }
		if (/\/api\/v1\/agents\/[^/]+\/models/.test(pathname)) {
			const codex = pathname.includes("/codex/");
			await route.fulfill({ json: { agent: codex ? "codex" : "opencode", selectionMode: "catalog", models: [{ id: codex ? "codex/model-a" : "provider/model-a", label: "Model A", provider: "fixture", isDefault: true, efforts: codex ? ["high", "xhigh"] : ["native-high", "native-ultra"], effortSupport: "supported", defaultEffort: codex ? "high" : "native-high" }], allowCustom: false, customModelEntry: "none", refreshRecommended: false } }); return;
		}
		await route.fulfill({ json: { status: "ok" } });
	});
	return { commands, savedPolicies, reconciliations, previews, exceptions, planRequests, statusReads: () => statusReads };
}

test("renderer: Instructor sits above projects and shows disabled capability honestly @T0 @INSTRUCTOR", async ({ page }) => {
	await fixture(page, false);
	await page.goto("/", { waitUntil: "domcontentloaded" });
	const nav = page.locator('[data-sidebar="menu-button"][aria-label="Instructor"]');
	await nav.click();
	await expect(nav).toBeVisible();
	const projects = page.getByText("Projects", { exact: true });
	expect((await nav.boundingBox())!.y).toBeLessThan((await projects.boundingBox())!.y);
	await expect(page).toHaveURL(/\/instructor$/);
	await expect(page.getByText("Instructor is disabled")).toBeVisible();
	await expect(page.getByText("Login flow", { exact: true })).toHaveCount(0);
});

test("renderer: frozen Work snapshot, inventory, exact publication and honest launch readback @T0 @INSTRUCTOR", async ({ page }, testInfo) => {
	const state = await fixture(page);
	await page.setViewportSize({ width: 1600, height: 1000 });
	await page.goto("/");
	await page.locator('[data-sidebar="menu-button"][aria-label="Instructor"]').click();
	await expect(page.getByRole("heading", { name: "Instructor", exact: true })).toBeVisible();
	await expect(page.getByTestId("instructor-topbar-label")).toHaveText("Instructor");
	await expect(page.getByText("work-login", { exact: true })).toBeVisible();
	await expect(page.getByText("Login flow", { exact: true })).toHaveCount(1);
	await expect(page.getByText("Closed · last 7 days")).toBeVisible();
	await expect(page.getByText("feature/orphan", { exact: false })).toBeVisible();
	await expect(page.getByText("Repository unavailable", { exact: true })).toBeVisible();
	await page.screenshot({ path: testInfo.outputPath("instructor-overview.png"), fullPage: true });
	await page.getByRole("tab", { name: /History/ }).click();
	await expect(page.getByText("Closed cleanup", { exact: true })).toBeVisible();
	await page.getByRole("tab", { name: /Parked/ }).click();
	await expect(page.getByText("Wait for database window", { exact: true })).toBeVisible();
	await page.getByRole("tab", { name: /Active/ }).click();
	await page.getByRole("button", { name: "Go all", exact: true }).click();
	await expect.poll(() => state.commands.length).toBe(1);
	expect(state.commands[0]).toMatchObject({ command: "go all", snapshotId: "snapshot-renderer-v1" });
	expect(state.commands[0]).not.toHaveProperty("workIds");
	expect(state.statusReads()).toBe(1);
	await expect(page.getByText("Skipped: work-publish: exact publication approval required")).toBeVisible();
	const publication = page.getByRole("row").filter({ hasText: "Publish upgrade" });
	await publication.getByRole("button", { name: "Go", exact: true }).click();
	const gate = page.getByRole("dialog", { name: "Go", exact: true });
	await expect(gate.getByRole("textbox")).toHaveValue("");
	await gate.getByRole("button", { name: "Go", exact: true }).click();
	expect(state.commands).toHaveLength(1);
	await gate.getByRole("textbox").fill("PUBLISH-GO feature/upgrade");
	await gate.getByRole("button", { name: "Go", exact: true }).click();
	await expect.poll(() => state.commands.length).toBe(2);
	expect(state.commands[1]).toMatchObject({ command: "go", snapshotId: "snapshot-renderer-v1", workIds: ["work-publish"], approval: "PUBLISH-GO feature/upgrade" });
	await expect(gate).not.toBeVisible();
	await page.getByRole("row").filter({ hasText: "Login flow" }).getByRole("button", { name: "Details", exact: true }).click();
	const details = page.getByRole("dialog", { name: "Login flow" });
	await expect(details.getByText("opencode · provider/model-a · native-ultra", { exact: true })).toBeVisible();
	await expect(details.getByText("opencode · provider/model-a · native-high", { exact: true })).toBeVisible();
	await expect(details.getByRole("row").filter({ hasText: "native-ultra" }).getByRole("cell", { name: "Unknown", exact: true })).toBeVisible();
});

test("renderer: policy editor pins provider-native model effort without weakening workflow gates @T0 @INSTRUCTOR", async ({ page }, testInfo) => {
	const state = await fixture(page);
	await page.setViewportSize({ width: 1400, height: 1600 });
	await page.goto("/#/instructor");
	await page.getByRole("button", { name: "Policy settings", exact: true }).click();
	const dialog = page.getByRole("dialog", { name: "Instructor policy settings" });
	await expect(dialog.getByRole("button", { name: "Save policy" })).toBeEnabled();
	await dialog.getByRole("button", { name: "worker Requested model / effort", exact: true }).click();
	await page.getByRole("menuitem", { name: /Reasoning effort/ }).hover();
	await page.getByRole("menuitemradio", { name: "Native-ultra", exact: true }).click();
	await dialog.getByRole("button", { name: "Save policy" }).click();
	await expect.poll(() => state.savedPolicies.length).toBe(1);
	const saved = state.savedPolicies[0] as typeof policy;
	expect(saved.roles.worker).toMatchObject({ model: "provider/model-a", effort: "maximum", effortByModel: { "provider/model-a": "native-ultra" } });
	expect(saved.transitions).toEqual(policy.transitions);
	expect(saved.closeoutRequires).toEqual(policy.closeoutRequires);
	expect(saved.profiles).toEqual(policy.profiles);
	await expect(dialog.getByText("Policy saved. Existing work keeps its pinned version.")).toBeVisible();
	await dialog.evaluate((element) => { element.scrollTop = 0; });
	await page.screenshot({ path: testInfo.outputPath("instructor-policy.png"), fullPage: true });
});

test("renderer: exact delivery observation restores the pending publication gate @T0 @INSTRUCTOR", async ({ page }) => {
	const state = await fixture(page, true, true);
	await page.setViewportSize({ width: 1400, height: 1100 });
	await page.goto("/#/instructor?workId=work-login");
	const details = page.getByRole("dialog", { name: "Login flow", exact: true });
	await details.getByRole("combobox", { name: "Observed delivery" }).selectOption("absent");
	await expect(details.getByText("Verification: waiting · passes 2 / 2")).toBeVisible();
	await expect(details.getByText("Original Human gate remains pending: Publish login")).toBeVisible();
	await expect(details.getByText("dispatch-exact · orch-exact")).toBeVisible();
	await details.getByRole("textbox", { name: "Observation note" }).fill("Inspected this exact target conversation; the dispatch is absent.");
	await details.getByRole("button", { name: "Record observation" }).click();
	await expect.poll(() => state.reconciliations.length).toBe(1);
	expect(state.reconciliations[0]).toMatchObject({ snapshotId: "snapshot-renderer-v1", workId: "work-login", dispatchId: "dispatch-exact", accepted: false });
	await expect(details.getByRole("button", { name: "Record observation" })).toHaveCount(0);
	await details.getByRole("button", { name: "Close", exact: true }).click();
	await page.getByRole("button", { name: "Status", exact: true }).click();
	await expect(page.getByText(/Snapshot snapshot-renderer-v2/)).toBeVisible();
	await expect(page.getByRole("row").filter({ hasText: "Login flow" }).getByText("Publish login", { exact: true })).toBeVisible();
	expect(state.commands.filter((command) => command.command !== "status")).toHaveLength(0);
});

test("renderer: scoped work exception uses a reviewed exact approval without publishing @T0 @INSTRUCTOR", async ({ page }, testInfo) => {
	const state = await fixture(page, true, false, true);
	await page.setViewportSize({ width: 1400, height: 1100 });
	await page.goto("/#/instructor?workId=work-login");
	const details = page.getByRole("dialog", { name: "Login flow", exact: true });
	await details.getByRole("button", { name: "Work exception", exact: true }).click();
	const exception = page.getByRole("dialog", { name: "Work exception", exact: true });
	await exception.getByRole("spinbutton").fill("4");
	await exception.getByRole("textbox", { name: "Reason for this exception" }).fill("One bounded extra review cycle for this work.");
	await exception.getByRole("button", { name: "Preview exception" }).click();
	await expect(exception.getByText("Effective fix-round budget: 4")).toBeVisible();
	await expect(exception.getByRole("textbox", { name: "Type the exact exception approval above" })).toHaveValue("");
	await expect(exception.getByRole("button", { name: "Grant this work exception" })).toBeDisabled();
	expect(state.exceptions).toHaveLength(0);
	await page.screenshot({ path: testInfo.outputPath("instructor-exception-preview.png"), fullPage: true });
	await exception.getByRole("textbox", { name: "Type the exact exception approval above" }).fill("EXCEPTION-GO work-login reviewed-proposal");
	await exception.getByRole("button", { name: "Grant this work exception" }).click();
	await expect.poll(() => state.exceptions.length).toBe(1);
	expect(state.exceptions[0]).toMatchObject({ ...state.previews[0], snapshotId: "snapshot-renderer-v1", workId: "work-login", approval: "EXCEPTION-GO work-login reviewed-proposal" });
	expect(state.commands.filter((command) => command.command !== "status")).toHaveLength(0);
});

test("renderer: strategic Instructor advice stays advisory and uses frozen gate admission @T0 @INSTRUCTOR", async ({ page }, testInfo) => {
	const state = await fixture(page);
	await page.setViewportSize({ width: 1600, height: 1100 });
	await page.goto("/#/instructor");
	await page.getByRole("textbox", { name: "Strategic request" }).fill("Continue ready work and explain publication gates");
	await page.getByRole("button", { name: "Ask Instructor", exact: true }).click();
	await expect(page.getByText("Ready work can continue; publication needs the operator.")).toBeVisible();
	expect(state.planRequests).toHaveLength(1);
	expect(state.planRequests[0]).toMatchObject({ request: "Continue ready work and explain publication gates", snapshotId: "snapshot-renderer-v1", idempotencyKey: expect.any(String) });
	expect(state.commands).toHaveLength(0);
	await expect(page.getByText("opencode · provider/model-a · Unknown", { exact: true })).toBeVisible();
	await page.screenshot({ path: testInfo.outputPath("instructor-strategy.png"), fullPage: true });
	await page.getByRole("button", { name: "Apply proposed command", exact: true }).click();
	const gate = page.getByRole("dialog", { name: "Go", exact: true });
	await expect(gate.getByRole("textbox")).toHaveValue("");
	expect(state.commands).toHaveLength(0);
	await gate.getByRole("textbox").fill("PUBLISH-GO feature/upgrade");
	await gate.getByRole("button", { name: "Go", exact: true }).click();
	await expect.poll(() => state.commands.length).toBe(1);
	expect(state.commands[0]).toMatchObject({ command: "go", snapshotId: "snapshot-renderer-v1", workIds: ["work-publish"], approval: "PUBLISH-GO feature/upgrade" });
	await page.getByRole("button", { name: "Review intake draft", exact: true }).nth(0).click();
	const intake = page.getByRole("dialog", { name: "New work", exact: true });
	await expect(intake.getByRole("textbox", { name: "Short work name" })).toHaveValue("Investigate retry");
	await expect(intake.getByRole("textbox", { name: "Issue reference", exact: true })).toHaveValue("456");
	await expect(intake.getByRole("textbox", { name: "Source / issue reference", exact: true })).toHaveValue("plan:plan-renderer:0");
	await intake.getByRole("button", { name: "Close", exact: true }).click();
	await page.getByRole("button", { name: "Review intake draft", exact: true }).nth(1).click();
	await expect(intake.getByRole("textbox", { name: "Short work name", exact: true })).toHaveValue("Improve retry visibility");
	await expect(intake.getByRole("textbox", { name: "Source / issue reference", exact: true })).toHaveValue("plan:plan-renderer:1");
	await intake.getByRole("button", { name: "Close", exact: true }).click();
	expect(state.commands).toHaveLength(1);
	await page.getByRole("button", { name: "Status", exact: true }).click();
	await expect(page.getByRole("button", { name: "Apply proposed command", exact: true })).toBeDisabled();
	await expect(page.getByText("This advice refers to an earlier snapshot. Ask again for the current portfolio before applying it.")).toBeVisible();
});
