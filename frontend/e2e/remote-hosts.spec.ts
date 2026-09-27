import { expect, test, type Page } from "@playwright/test";
import { installFakeBridge, type FakeRemoteHost } from "./support/fake-bridge";

// Renderer boundary: two saved hosts deliberately reuse project-1/session-1.
// The main-process proxy and real daemon are covered separately; page.route
// supplies each host's HTTP responses here.
const hosts: FakeRemoteHost[] = [
	{ hostId: "box-a", label: "Box A", url: "http://box-a:3011", base: "http://127.0.0.1:44121" },
	{ hostId: "box-b", label: "Box B", url: "http://box-b:3011", base: "http://127.0.0.1:44122" },
];

type RemoteState = { messages: Record<string, string[]>; actions: string[]; spawned: Record<string, string[]> };

async function setupClient(page: Page, state: RemoteState): Promise<void> {
	await page.addInitScript(() => window.localStorage.setItem("ao.remoteHosts", "true"));
	await installFakeBridge(page, { remoteHosts: hosts });
	for (const host of hosts) {
		await page.route(`${host.base}/api/v1/**`, async (route) => {
			const request = route.request();
			const path = new URL(request.url()).pathname;
			const headers = {
				"access-control-allow-origin": "*",
				"access-control-allow-methods": "GET, POST, OPTIONS",
				"access-control-allow-headers": "content-type",
				"content-type": "application/json",
			};
			if (request.method() === "OPTIONS") {
				await route.fulfill({ status: 204, headers });
				return;
			}
			let body: unknown;
			if (path === "/api/v1/projects") body = { projects: [{ id: "project-1", name: `${host.label} project`, path: "/repo" }] };
			else if (path === "/api/v1/sessions" && request.method() === "GET") body = { sessions: [
				{ id: "session-1", projectId: "project-1", displayName: `${host.label} task`, harness: "codex", status: "working", mode: "chat", prs: [] },
				...state.spawned[host.hostId].map((id) => ({ id, displayName: "Started remotely", harness: "codex", status: "working", mode: "chat", prs: [] })),
			] };
			else if (path === "/api/v1/agents/readiness/ensure") body = { agents: [{ id: "codex", label: "Codex", effectiveReadiness: "ready" }] };
			else if (path === "/api/v1/settings") body = { chatHarnesses: ["codex"] };
			else if (path === "/api/v1/sessions" && request.method() === "POST") {
				state.actions.push(`${host.hostId}:spawn`);
				const id = `started-${host.hostId}`;
				state.spawned[host.hostId].push(id);
				body = { session: { id }, promptBytes: 10, systemPromptBytes: 0 };
			}
			else if (path.endsWith("/conversation") && request.method() === "GET") body = {
				messages: state.messages[host.hostId].map((text, index) => ({ id: `${host.hostId}-${index}`, role: index ? "user" : "assistant", text, sequence: index + 1 })),
				activities: [],
			};
			else if (path.endsWith("/conversation/messages") && request.method() === "POST") {
				state.actions.push(`${host.hostId}:send`);
				state.messages[host.hostId].push(request.postDataJSON().text);
				body = { duplicate: false, state: "running", turnId: `${host.hostId}-turn` };
			} else if (path.endsWith("/kill") && request.method() === "POST") {
				state.actions.push(`${host.hostId}:kill`);
				body = {};
			} else throw new Error(`Unexpected ${request.method()} ${request.url()}`);
			await route.fulfill({ status: path === "/api/v1/sessions" && request.method() === "POST" ? 201 : request.method() === "POST" ? 202 : 200, headers, body: JSON.stringify(body) });
		});
	}
}

test("two desktop clients continue Box A without confusing Box B's identical session ID @remote", async ({ page }) => {
	const state: RemoteState = {
		messages: { "box-a": ["A is working"], "box-b": ["B is working"] },
		actions: [],
		spawned: { "box-a": [], "box-b": [] },
	};
	await setupClient(page, state);
	await page.goto("/#/");
	const section = page.getByTestId("remote-hosts-section");
	await expect(section.getByTestId("remote-session-row")).toHaveCount(2);
	await section.locator('[data-host-id="box-a"]').getByTestId("remote-session-row").click();
	await expect(page.getByTestId("remote-session-view")).toHaveAttribute("data-host-id", "box-a");
	await expect(page.getByText("A is working")).toBeVisible();
	await page.getByRole("textbox", { name: "Message" }).fill("Continue on A");
	await page.getByRole("button", { name: "Send", exact: true }).click();
	await expect.poll(() => state.actions).toContain("box-a:send");

	// A second laptop loads the same host-owned conversation after the first
	// client sends. The session lives on A, not in either renderer.
	const second = await page.context().newPage();
	await setupClient(second, state);
	await second.goto("/#/host/box-a/project/project-1/session/session-1");
	await expect(second.getByText("Continue on A")).toBeVisible();
	await second.close();

	await section.locator('[data-host-id="box-b"]').getByTestId("remote-session-row").click();
	await expect(page.getByTestId("remote-session-view")).toHaveAttribute("data-host-id", "box-b");
	await expect(page.getByText("B is working")).toBeVisible();
	page.once("dialog", (dialog) => dialog.accept());
	await page.getByRole("button", { name: "Stop session" }).click();
	await expect.poll(() => state.actions).toContain("box-b:kill");
	expect(state.actions).not.toContain("box-a:kill");

	await section.locator('[data-host-id="box-a"]').getByRole("button", { name: "Start on Box A" }).click();
	await expect(page.getByRole("heading", { name: "Start on Box A" })).toBeVisible();
	await page.keyboard.press("Escape");
	await section.locator('[data-host-id="box-b"]').getByRole("button", { name: "Start on Box B" }).click();
	await expect(page.getByRole("heading", { name: "Start on Box B" })).toBeVisible();
	await page.getByRole("textbox", { name: "Task" }).fill("Investigate auth");
	await page.getByRole("button", { name: "Start on remote host" }).click();
	await expect.poll(() => state.actions).toContain("box-b:spawn");
	await expect(page).toHaveURL(/\/host\/box-b\/session\/started-box-b/);
	expect(state.actions).not.toContain("box-a:spawn");
});
