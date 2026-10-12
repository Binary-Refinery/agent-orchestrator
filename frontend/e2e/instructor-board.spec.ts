import { expect, test, type Page } from "@playwright/test";
import { agentReadiness } from "../src/renderer/test/agent-readiness-fixtures";
import { installFakeAgent } from "./support/fake-bridge";

// Real renderer and Chromium, deterministic HTTP fixtures only. Daemon
// membership/admission/persistence tests cover the corresponding live boundary.
async function fixture(page: Page) {
	await installFakeAgent(page, { projectId: "board-project", projectName: "Board project" });
	const at = "2026-10-12T08:00:00Z";
	const commands = [
		{ action: "go", name: "Go", aliases: [], mutates: true, snapshotRequired: true },
		{ action: "weiter", name: "Weiter", aliases: [], mutates: true, snapshotRequired: true },
		{ action: "neu", name: "Neu", aliases: [], mutates: true, snapshotRequired: false },
	];
	const policy = { schema: 1, version: "policy-1", initialPhase: "intake", roles: {
		instructor: { harness: "opencode", model: "provider/model-a", effort: "maximum", effortByModel: { "provider/model-a": "native-high", "provider/model-b": "native-deep" } },
		worker: { harness: "opencode", model: "provider/model-a", effort: "maximum", effortByModel: { "provider/model-a": "native-high" } },
	}, profiles: { full: { command: ["npm", "test"], timeoutSeconds: 120 } }, transitions: [], closeoutRequires: ["full", "review", "delta"], maxFixRounds: 2 };
	let version = 1;
	let reads = 0;
	const sent: Array<Record<string, unknown>> = [];
	const saved: Array<typeof policy> = [];
	const works = () => [
		{ id: "work-ready", projectId: "board-project", name: "Login flow", goal: "Reliable login", state: "ready", phase: "implementation", sourceRef: "#123", policyVersion: "policy-1", policy, actions: ["weiter"], next: version === 1 ? "Build the login flow" : "Changed progress", progressAt: at, revision: version, evidence: [] },
		{ id: "work-publish", projectId: "board-project", name: "Publish release", goal: "Reviewed release", state: "waiting_human", phase: "publication", sourceRef: "#124", policyVersion: "policy-1", policy, sha: version === 1 ? "captured-commit" : "new-commit", branch: version === 1 ? "feature/release" : "feature/changed", actions: ["go"], next: "Review release gate", progressAt: at, revision: version, gate: { id: "gate-publish", kind: "publication", title: "Publish reviewed release", exactApproval: version === 1 ? "PUBLISH-GO feature/release" : "PUBLISH-GO feature/changed" }, evidence: [] },
		{ id: "work-second", projectId: "other-project", name: "Review data model with a long readable name", goal: "Review changes", state: "ready", phase: "review", sourceRef: "#456", policyVersion: "policy-1", policy, actions: ["weiter"], next: "Inspect changes independently", progressAt: at, revision: 1, evidence: [] },
	];
	await page.route("http://127.0.0.1:8080/api/v1/instructor/**", async (route) => {
		const request = route.request(), path = new URL(request.url()).pathname;
		if (path.endsWith("/capabilities")) { await route.fulfill({ json: { enabled: true, commands, publication: { available: true } } }); return; }
		if (path.endsWith("/status")) { reads++; await route.fulfill({ json: { id: `snapshot-${version}`, createdAt: at, works: works(), commands } }); return; }
		if (path.endsWith("/inventory")) { await route.fulfill({ json: { capturedAt: at, projects: [{ id: "board-project", name: "Board project" }, { id: "other-project", name: "Other project" }], sessions: [], resources: [] } }); return; }
		if (path.endsWith("/policy")) { if (request.method() === "PUT") saved.push(request.postDataJSON()); await route.fulfill({ json: saved.at(-1) ?? policy }); return; }
		if (path.endsWith("/commands")) { sent.push(request.postDataJSON()); await route.fulfill({ json: { dispatches: [], skipped: [] } }); return; }
		if (path.includes("/works/")) { await route.fulfill({ json: { work: works().find((work) => work.id === path.split("/").at(-1)), events: [], dispatches: [] } }); return; }
		await route.fulfill({ json: { jobs: [] } });
	});
	await page.route("http://127.0.0.1:8080/api/v1/agents/**", async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path.includes("readiness")) { await route.fulfill({ json: { agents: [agentReadiness("opencode", "OpenCode")] } }); return; }
		await route.fulfill({ json: { models: [{ id: "provider/model-a", label: "Model A", efforts: ["native-high", "native-ultra"], effortSupport: "supported" }, { id: "provider/model-b", label: "Model B", efforts: ["native-standard", "native-deep"], effortSupport: "supported" }], allowCustom: false } });
	});
	return { sent, saved, policy, change: () => { version++; }, reads: () => reads };
}

test("renderer: Instructor board adapts to the actual remaining container width @T0 @INSTRUCTOR_BOARD", async ({ page }, testInfo) => {
	await fixture(page);
	await page.goto("/#/instructor-board");
	await expect(page.getByRole("heading", { name: "Instructor board", exact: true })).toBeVisible();
	await expect(page.getByTestId("instructor-topbar-label")).toHaveText("Instructor board");
	for (const [width, height] of [[1600, 1000], [960, 600], [640, 480]]) {
		await page.setViewportSize({ width, height });
		const board = page.getByTestId("instructor-board");
		await expect(board.getByRole("article", { name: "Login flow", exact: true })).toBeVisible();
		expect(await board.evaluate((node) => node.scrollWidth <= node.clientWidth + 1)).toBe(true);
		const cards = board.locator(".instructor-board-work");
		for (const card of await cards.all()) expect(await card.evaluate((node) => node.scrollWidth <= node.clientWidth + 1)).toBe(true);
		await page.screenshot({ path: testInfo.outputPath(`board-${width}-${height}.png`), fullPage: true });
	}
	await page.getByRole("button", { name: "Review gate", exact: true }).click();
	const dialog = page.getByRole("dialog", { name: "Review approvals" });
	expect(await dialog.evaluate((node) => node.scrollHeight > node.clientHeight)).toBe(true);
	expect((await dialog.boundingBox())!.height).toBeLessThanOrEqual(448);
	await dialog.getByRole("checkbox", { name: "Approve Publish release" }).click();
	await expect(dialog.getByRole("button", { name: "Sign and continue" })).toBeEnabled();
});

test("renderer: live polling preserves an opened exact approval and refreshes after signing @T0 @INSTRUCTOR_BOARD", async ({ page }) => {
	const state = await fixture(page);
	await page.clock.install();
	await page.goto("/#/instructor-board");
	await page.getByRole("button", { name: "Review gate", exact: true }).click();
	const dialog = page.getByRole("dialog", { name: "Review approvals" });
	state.change(); await page.clock.fastForward(16_000);
	await expect.poll(state.reads).toBeGreaterThanOrEqual(2);
	await expect(dialog.getByText("captured-commit", { exact: true })).toBeVisible();
	await expect(dialog.getByText("new-commit", { exact: true })).toHaveCount(0);
	await dialog.getByRole("checkbox", { name: "Approve Publish release" }).click();
	await dialog.getByRole("button", { name: "Sign and continue" }).click();
	await expect.poll(() => state.sent.length).toBe(1);
	expect(state.sent[0]).toMatchObject({ command: "go", snapshotId: "snapshot-1", workIds: ["work-publish"], approvals: [{ workId: "work-publish", approval: "PUBLISH-GO feature/release" }] });
	await expect(dialog).not.toBeVisible();
	await expect(page.getByText("Changed progress", { exact: true })).toBeVisible();
});

test("renderer: top Instructor picker keeps model-specific effort and other policy roles @T0 @INSTRUCTOR_BOARD", async ({ page }) => {
	const state = await fixture(page);
	await page.goto("/#/instructor-board");
	await page.getByRole("button", { name: /Instructor model.*opencode/ }).click();
	const dialog = page.getByRole("dialog", { name: "Instructor model", exact: true });
	await dialog.getByRole("button", { name: "instructor Requested model / effort" }).click();
	await page.getByRole("menuitem", { name: /Model B/ }).click();
	await expect(page.getByRole("menuitemradio", { name: "Native-deep", exact: true })).toHaveAttribute("aria-checked", "true");
	await page.getByRole("menuitemradio", { name: "Native-deep", exact: true }).click();
	await expect(dialog.getByText(/native-deep/)).toBeVisible();
	await dialog.getByRole("button", { name: "Save model selection" }).click();
	await expect.poll(() => state.saved.length).toBe(1);
	expect(state.saved[0].roles.instructor).toMatchObject({ model: "provider/model-b", effortByModel: { "provider/model-a": "native-high", "provider/model-b": "native-deep" } });
	expect(state.saved[0].roles.worker).toEqual(state.policy.roles.worker);
	expect(state.saved[0].profiles).toEqual(state.policy.profiles);
	expect(state.saved[0].closeoutRequires).toEqual(state.policy.closeoutRequires);
});
