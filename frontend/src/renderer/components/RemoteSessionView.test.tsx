import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";

const { localGet, localPost, remoteConnect } = vi.hoisted(() => ({ localGet: vi.fn(), localPost: vi.fn(), remoteConnect: vi.fn() }));
vi.mock("../lib/api-client", () => ({ apiClient: { GET: localGet, POST: localPost }, hasTrustedApiBaseUrl: () => true }));
vi.mock("../lib/bridge", () => ({ aoBridge: { remotes: { connect: remoteConnect, disconnect: vi.fn() } } }));
vi.mock("../lib/telemetry", () => ({ captureRendererEvent: vi.fn() }));
vi.mock("../lib/agent-switch-visibility", () => ({ agentSwitchVisibility: { setQueryHealthy: vi.fn() } }));
vi.mock("../hooks/useCloudCp", () => ({ useCloudCp: () => ({ ready: false, baseUrl: "", client: {} }) }));
vi.mock("../hooks/useCloudOrg", () => ({ useCloudOrg: () => ({ org: undefined, ready: false }) }));
vi.mock("./RemoteTerminalView", () => ({ RemoteTerminalView: ({ proxyBase }: { proxyBase: string }) => <div data-testid="remote-terminal-base">{proxyBase}</div> }));

import { connectHost, disconnectHost } from "../lib/host-clients";
import { RemoteSessionView } from "./RemoteSessionView";

afterEach(async () => {
	await disconnectHost("box-a");
	vi.unstubAllGlobals();
});

it("reads and sends a remote Chat message through Box A, never the local daemon", async () => {
	const requests: Array<{ url: string; method: string }> = [];
	localGet.mockReset();
	localPost.mockReset();
	remoteConnect.mockResolvedValue({ hostId: "box-a", label: "Box A", url: "http://box-a:3001", base: "http://127.0.0.1:4000" });
	vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
		const request = input instanceof Request ? input : new Request(input);
		requests.push({ url: request.url, method: request.method });
		let body: unknown;
		if (request.url.endsWith("/projects")) body = { projects: [{ id: "project-1", name: "Remote", path: "/remote" }] };
		else if (request.url.endsWith("/sessions")) body = { sessions: [{ id: "session-1", projectId: "project-1", displayName: "Fix login", harness: "codex", status: "working", mode: "chat", prs: [] }] };
		else if (request.url.endsWith("/conversation")) body = { messages: [{ id: "msg-1", role: "assistant", text: "I am working on login", sequence: 1 }], activities: [] };
		else body = { state: "accepted", turnId: "turn-2" };
		return new Response(JSON.stringify(body), { status: request.method === "POST" ? 202 : 200, headers: { "content-type": "application/json" } });
	}));
	await connectHost("http://box-a:3001");
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	render(<QueryClientProvider client={queryClient}><RemoteSessionView hostId="box-a" sessionId="session-1" /></QueryClientProvider>);
	await screen.findByText("I am working on login");
	fireEvent.change(screen.getByRole("textbox", { name: "Message" }), { target: { value: "Please continue" } });
	fireEvent.click(screen.getByRole("button", { name: "Send" }));
	await waitFor(() => expect(requests).toContainEqual({
		url: "http://127.0.0.1:4000/api/v1/sessions/session-1/conversation/messages", method: "POST",
	}));
	expect(localGet).not.toHaveBeenCalled();
	expect(localPost).not.toHaveBeenCalled();
});

it("reattaches a terminal when the same host gets a new proxy connection", async () => {
	remoteConnect.mockReset()
		.mockResolvedValueOnce({ hostId: "box-a", label: "Box A", url: "http://box-a:3001", base: "http://127.0.0.1:4000/old" })
		.mockResolvedValueOnce({ hostId: "box-a", label: "Box A", url: "http://box-a:3001", base: "http://127.0.0.1:4001/new" });
	vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
		const request = input instanceof Request ? input : new Request(input);
		const body = request.url.endsWith("/projects")
			? { projects: [{ id: "project-1", name: "Remote", path: "/remote" }] }
			: { sessions: [{ id: "session-1", projectId: "project-1", displayName: "Worker", harness: "codex", status: "working", mode: "tui", terminalHandleId: "terminal-1", prs: [] }] };
		return new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
	}));
	await connectHost("http://box-a:3001");
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	render(<QueryClientProvider client={queryClient}><RemoteSessionView hostId="box-a" sessionId="session-1" /></QueryClientProvider>);
	expect(await screen.findByTestId("remote-terminal-base")).toHaveTextContent("http://127.0.0.1:4000/old");
	await connectHost("http://box-a:3001");
	await waitFor(() => expect(screen.getByTestId("remote-terminal-base")).toHaveTextContent("http://127.0.0.1:4001/new"));
});

it("reuses a Chat delivery ID when a lost response is retried", async () => {
	const deliveryIds: string[] = [];
	remoteConnect.mockResolvedValue({ hostId: "box-a", label: "Box A", url: "http://box-a:3001", base: "http://127.0.0.1:4000" });
	vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
		const request = input instanceof Request ? input : new Request(input);
		let body: unknown;
		let status = 200;
		if (request.url.endsWith("/projects")) body = { projects: [{ id: "project-1", name: "Remote", path: "/remote" }] };
		else if (request.url.endsWith("/sessions")) body = { sessions: [{ id: "session-1", projectId: "project-1", harness: "codex", status: "working", mode: "chat", prs: [] }] };
		else if (request.url.endsWith("/conversation")) body = { messages: [], activities: [] };
		else {
			deliveryIds.push((await request.json() as { clientMessageId: string }).clientMessageId);
			status = deliveryIds.length === 1 ? 503 : 202;
			body = status === 503 ? { error: "response lost" } : { state: "accepted", turnId: "turn-1" };
		}
		return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
	}));
	await connectHost("http://box-a:3001");
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	render(<QueryClientProvider client={queryClient}><RemoteSessionView hostId="box-a" sessionId="session-1" /></QueryClientProvider>);
	const message = await screen.findByRole("textbox", { name: "Message" });
	fireEvent.change(message, { target: { value: "Continue the task" } });
	fireEvent.click(screen.getByRole("button", { name: "Send" }));
	await screen.findByText("Could not send the message.");
	fireEvent.click(screen.getByRole("button", { name: "Send" }));
	await waitFor(() => expect(deliveryIds).toHaveLength(2));
	expect(deliveryIds[1]).toBe(deliveryIds[0]);
});

it.each([204, 409])("answers a remote approval through its host and refreshes after HTTP %s", async (status) => {
	const decisions: Array<{ url: string; decisionId: string }> = [];
	let conversationReads = 0;
	localGet.mockReset();
	localPost.mockReset();
	remoteConnect.mockResolvedValue({ hostId: "box-a", label: "Box A", url: "http://box-a:3001", base: "http://127.0.0.1:4000" });
	vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
		const request = input instanceof Request ? input : new Request(input);
		let body: unknown;
		if (request.url.endsWith("/projects")) body = { projects: [{ id: "project-1", name: "Remote", path: "/remote" }] };
		else if (request.url.endsWith("/sessions")) body = { sessions: [{ id: "session-1", projectId: "project-1", harness: "codex", status: "working", mode: "chat", prs: [] }] };
		else if (request.url.endsWith("/conversation")) {
			conversationReads++;
			body = { messages: [], activities: conversationReads === 1 ? [{
				kind: "activity", id: "approval-1", turnId: "turn-1", sequence: 1, revision: 1,
				activityKind: "approval", status: "pending", summary: "Run command", requestId: "acp:host:1",
				detail: { command: "npm test", decisions: [{ id: "allow-once", label: "Allow once", kind: "allow_once" }] },
				createdAt: "2026-09-28T00:00:00Z",
			}] : [] };
		} else if (request.url.endsWith("/resolve")) {
			decisions.push({ url: request.url, decisionId: (await request.json() as { decisionId: string }).decisionId });
			return status === 204
				? new Response(null, { status })
				: new Response(JSON.stringify({ code: "CHAT_REQUEST_NOT_PENDING", message: "already answered" }), { status, headers: { "content-type": "application/json" } });
		} else throw new Error(`Unexpected request ${request.url}`);
		return new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
	}));
	await connectHost("http://box-a:3001");
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	render(<QueryClientProvider client={queryClient}><RemoteSessionView hostId="box-a" sessionId="session-1" /></QueryClientProvider>);
	const approval = await screen.findByRole("group", { name: "Approval request acp:host:1" });
	expect(screen.queryByRole("textbox", { name: "Message" })).not.toBeInTheDocument();
	fireEvent.click(within(approval).getByRole("button", { name: /Allow once/ }));
	await waitFor(() => expect(decisions).toEqual([{
		url: "http://127.0.0.1:4000/api/v1/sessions/session-1/conversation/approvals/acp%3Ahost%3A1/resolve",
		decisionId: "allow-once",
	}]));
	await waitFor(() => expect(screen.queryByRole("group", { name: "Approval request acp:host:1" })).not.toBeInTheDocument());
	expect(localPost).not.toHaveBeenCalled();
	expect(localGet).not.toHaveBeenCalled();
});
