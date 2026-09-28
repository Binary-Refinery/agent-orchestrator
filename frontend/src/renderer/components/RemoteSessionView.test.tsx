import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { typeInLexicalEditor } from "../test/lexical";

const { localGet, localPost, remoteConnect } = vi.hoisted(() => ({ localGet: vi.fn(), localPost: vi.fn(), remoteConnect: vi.fn() }));
vi.mock("../lib/api-client", async (importOriginal) => ({
	...await importOriginal<typeof import("../lib/api-client")>(),
	apiClient: { GET: localGet, POST: localPost },
	hasTrustedApiBaseUrl: () => true,
}));
vi.mock("../lib/bridge", async (importOriginal) => {
	const actual = await importOriginal<typeof import("../lib/bridge")>();
	return { ...actual, aoBridge: { ...actual.aoBridge, remotes: { connect: remoteConnect, disconnect: vi.fn() } } };
});
vi.mock("../lib/telemetry", () => ({ captureRendererEvent: vi.fn() }));
vi.mock("../lib/agent-switch-visibility", () => ({ agentSwitchVisibility: { setQueryHealthy: vi.fn() } }));
vi.mock("../hooks/useCloudCp", () => ({ useCloudCp: () => ({ ready: false, baseUrl: "", client: {} }) }));
vi.mock("../hooks/useCloudOrg", () => ({ useCloudOrg: () => ({ org: undefined, ready: false }) }));
vi.mock("./RemoteTerminalView", () => ({ RemoteTerminalView: ({ proxyBase }: { proxyBase: string }) => <div data-testid="remote-terminal-base">{proxyBase}</div> }));

import { connectHost, disconnectHost } from "../lib/host-clients";
import { RemoteSessionView } from "./RemoteSessionView";
import { SessionTopbarProvider } from "./SessionTopbarPortal";
import { TooltipProvider } from "./ui/tooltip";

function renderRemoteSession(queryClient: QueryClient) {
	return render(<QueryClientProvider client={queryClient}>
		<TooltipProvider><SessionTopbarProvider><RemoteSessionView hostId="box-a" sessionId="session-1" /></SessionTopbarProvider></TooltipProvider>
	</QueryClientProvider>);
}

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
		else if (new URL(request.url).pathname.endsWith("/conversation")) body = { messages: [{ id: "msg-1", role: "assistant", text: "I am working on login", sequence: 1 }], activities: [] };
		else body = { state: "accepted", turnId: "turn-2" };
		return new Response(JSON.stringify(body), { status: request.method === "POST" ? 202 : 200, headers: { "content-type": "application/json" } });
	}));
	await connectHost("http://box-a:3001");
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	renderRemoteSession(queryClient);
	await screen.findByText("I am working on login");
	await typeInLexicalEditor(screen.getByRole("combobox", { name: "Message the agent" }), "Please continue");
	await userEvent.click(screen.getByRole("button", { name: "Send message" }));
	await waitFor(() => expect(requests).toContainEqual({
		url: "http://127.0.0.1:4000/api/v1/sessions/session-1/conversation/messages", method: "POST",
	}));
	expect(localGet).not.toHaveBeenCalled();
	expect(localPost).not.toHaveBeenCalled();
});

it("shows a normal inspector and reads its changed files from the remote host only", async () => {
	const requests: string[] = [];
	localGet.mockReset();
	remoteConnect.mockResolvedValue({ hostId: "box-a", label: "Box A", url: "http://box-a:3001", base: "http://127.0.0.1:4000" });
	vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
		const request = input instanceof Request ? input : new Request(input);
		const path = new URL(request.url).pathname;
		requests.push(request.url);
		if (path.endsWith("/projects")) return Response.json({ projects: [{ id: "project-1", name: "Remote", path: "/remote" }] });
		if (path.endsWith("/sessions")) return Response.json({ sessions: [{ id: "session-1", projectId: "project-1", displayName: "Fix login", harness: "codex", status: "working", mode: "chat", branch: "fix/login", prs: [{ url: "https://github.com/acme/app/pull/42", number: 42, state: "open", ci: "passing", review: "none", mergeability: "mergeable", reviewComments: false, updatedAt: "2026-09-28T00:00:00Z" }] }] });
		if (path.endsWith("/conversation")) return Response.json({ messages: [], activities: [] });
		if (path.endsWith("/workspace/files")) return Response.json({ files: [{ path: "app/page.tsx", status: "modified", additions: 1, deletions: 1 }] });
		if (path.endsWith("/workspace/file")) return Response.json({ path: "app/page.tsx", diff: "@@ -1 +1 @@\n-old\n+new", content: "new", binary: false, contentTruncated: false, diffTruncated: false });
		throw new Error(`Unexpected request ${request.url}`);
	}));
	await connectHost("http://box-a:3001");
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	renderRemoteSession(queryClient);
	expect(await screen.findByRole("complementary", { name: "Session inspector" })).toBeInTheDocument();
	expect(screen.getByText("Box A", { selector: "dd" })).toBeInTheDocument();
	expect(screen.getByText("fix/login", { selector: "dd" })).toBeInTheDocument();
	expect(screen.getByRole("link", { name: /PR #42/ })).toHaveAttribute("href", "https://github.com/acme/app/pull/42");
	await userEvent.click(screen.getByRole("tab", { name: "Files" }));
	await userEvent.click(await screen.findByRole("button", { name: /app\/page\.tsx/ }));
	expect(await screen.findByText(/\+new/)).toBeInTheDocument();
	expect(requests).toContain("http://127.0.0.1:4000/api/v1/sessions/session-1/workspace/files");
	expect(requests.some((url) => url.startsWith("http://127.0.0.1:4000/api/v1/sessions/session-1/workspace/file?path="))).toBe(true);
	expect(localGet).not.toHaveBeenCalled();
	await userEvent.click(screen.getByRole("button", { name: "Close inspector panel" }));
	expect(screen.queryByRole("complementary", { name: "Session inspector" })).not.toBeInTheDocument();
	await userEvent.click(screen.getByRole("button", { name: "Open inspector panel" }));
	expect(screen.getByRole("complementary", { name: "Session inspector" })).toBeInTheDocument();
});

it("loads older remote history once while polling only the latest page", async () => {
	const conversationReads: URL[] = [];
	let latestReads = 0;
	localGet.mockReset();
	remoteConnect.mockResolvedValue({ hostId: "box-a", label: "Box A", url: "http://box-a:3001", base: "http://127.0.0.1:4000" });
	vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
		const request = input instanceof Request ? input : new Request(input);
		const url = new URL(request.url);
		let body: unknown;
		if (url.pathname.endsWith("/projects")) body = { projects: [{ id: "project-1", name: "Remote", path: "/remote" }] };
		else if (url.pathname.endsWith("/sessions")) body = { sessions: [{ id: "session-1", projectId: "project-1", harness: "codex", status: "working", mode: "chat", prs: [] }] };
		else if (url.pathname.endsWith("/conversation")) {
			conversationReads.push(url);
			if (url.searchParams.has("beforeSequence")) {
				body = { sessionId: "session-1", controller: "running", latestSequence: 200, oldestSequence: 1, hasMoreBefore: false,
					messages: [{ id: "older", role: "assistant", text: "Earlier work", sequence: 200 }], activities: [] };
			} else {
				latestReads++;
				body = { sessionId: "session-1", controller: "running", latestSequence: 400 + latestReads, oldestSequence: 200 + latestReads, hasMoreBefore: true,
					messages: [
						...(latestReads === 1 ? [{ id: "edge", role: "assistant", text: "Sliding window edge", sequence: 201 }] : []),
						{ id: "latest", role: "assistant", text: `Latest update ${latestReads}`, sequence: 400 + latestReads },
					], activities: [] };
			}
		} else throw new Error(`Unexpected request ${request.url}`);
		return new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
	}));
	await connectHost("http://box-a:3001");
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	renderRemoteSession(queryClient);
	await screen.findByText("Latest update 1");
	fireEvent.click(screen.getByRole("button", { name: "Load earlier messages" }));
	await screen.findByText("Earlier work");
	expect(conversationReads.map((url) => url.searchParams.get("beforeSequence"))).toContain("201");
	expect(conversationReads.every((url) => url.searchParams.get("limit") === "200")).toBe(true);
	await screen.findByText("Latest update 2", {}, { timeout: 4_000 });
	expect(screen.getByText("Sliding window edge")).toBeInTheDocument();
	expect(screen.getByText("Earlier work")).toBeInTheDocument();
	expect(conversationReads.filter((url) => url.searchParams.has("beforeSequence"))).toHaveLength(1);
	expect(localGet).not.toHaveBeenCalled();
});

it("interrupts the turn and stops the session on Box A, not the local daemon", async () => {
	const posts: string[] = [];
	localGet.mockReset();
	localPost.mockReset();
	remoteConnect.mockResolvedValue({ hostId: "box-a", label: "Box A", url: "http://box-a:3001", base: "http://127.0.0.1:4000" });
	const confirm = vi.fn(() => true);
	vi.stubGlobal("confirm", confirm);
	vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
		const request = input instanceof Request ? input : new Request(input);
		const path = new URL(request.url).pathname;
		if (request.method === "POST") {
			posts.push(request.url);
			return new Response(null, { status: 204 });
		}
		const body = path.endsWith("/projects")
			? { projects: [{ id: "project-1", name: "Remote", path: "/remote" }] }
			: path.endsWith("/sessions")
				? { sessions: [{ id: "session-1", projectId: "project-1", displayName: "Worker", harness: "codex", status: "working", mode: "chat", prs: [] }] }
				: { sessionId: "session-1", controller: "running", latestSequence: 1, turns: [{ id: "turn-1", state: "running", requestedAt: "2026-09-28T00:00:00Z" }], messages: [], activities: [] };
		return new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
	}));
	await connectHost("http://box-a:3001");
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	renderRemoteSession(queryClient);
	fireEvent.click(await screen.findByRole("button", { name: "Stop turn" }));
	await waitFor(() => expect(posts).toContain("http://127.0.0.1:4000/api/v1/sessions/session-1/conversation/interrupt"));
	fireEvent.click(screen.getByRole("button", { name: "Stop session" }));
	await waitFor(() => expect(posts).toContain("http://127.0.0.1:4000/api/v1/sessions/session-1/kill"));
	expect(confirm).toHaveBeenCalledWith("Stop Worker on box-a?");
	expect(localGet).not.toHaveBeenCalled();
	expect(localPost).not.toHaveBeenCalled();
});

it("keeps accepted remote sends accepted when the follow-up read fails", async () => {
	let conversationReads = 0;
	remoteConnect.mockResolvedValue({ hostId: "box-a", label: "Box A", url: "http://box-a:3001", base: "http://127.0.0.1:4000" });
	vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
		const request = input instanceof Request ? input : new Request(input);
		const path = new URL(request.url).pathname;
		if (path.endsWith("/projects")) return Response.json({ projects: [{ id: "project-1", name: "Remote", path: "/remote" }] });
		if (path.endsWith("/sessions")) return Response.json({ sessions: [{ id: "session-1", projectId: "project-1", harness: "codex", status: "working", mode: "chat", prs: [] }] });
		if (path.endsWith("/conversation")) {
			conversationReads++;
			return conversationReads === 1
				? Response.json({ sessionId: "session-1", controller: "running", latestSequence: 1, messages: [{ id: "first", role: "assistant", text: "Still here", sequence: 1 }], activities: [] })
				: Response.json({ code: "UNAVAILABLE", message: "Connection lost" }, { status: 503 });
		}
		if (path.endsWith("/conversation/messages")) return Response.json({ state: "accepted", turnId: "turn-2" }, { status: 202 });
		throw new Error(`Unexpected request ${request.url}`);
	}));
	await connectHost("http://box-a:3001");
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	renderRemoteSession(queryClient);
	await screen.findByText("Still here");
	await typeInLexicalEditor(screen.getByRole("combobox", { name: "Message the agent" }), "Continue");
	fireEvent.click(screen.getByRole("button", { name: "Send message" }));
	expect(await screen.findByText("Could not load this remote conversation.")).toHaveAttribute("role", "alert");
	expect(screen.getByText("Still here")).toBeInTheDocument();
	expect(screen.queryByText(/delivery wasn’t confirmed/)).not.toBeInTheDocument();
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
	renderRemoteSession(queryClient);
	expect(await screen.findByTestId("remote-terminal-base")).toHaveTextContent("http://127.0.0.1:4000/old");
	await act(async () => { await connectHost("http://box-a:3001"); });
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
		else if (new URL(request.url).pathname.endsWith("/conversation")) body = { messages: [], activities: [] };
		else {
			deliveryIds.push((await request.json() as { clientMessageId: string }).clientMessageId);
			status = deliveryIds.length === 1 ? 503 : 202;
			body = status === 503 ? { error: "response lost" } : { state: "accepted", turnId: "turn-1" };
		}
		return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
	}));
	await connectHost("http://box-a:3001");
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	renderRemoteSession(queryClient);
	const message = await screen.findByRole("combobox", { name: "Message the agent" });
	await typeInLexicalEditor(message, "Continue the task");
	await userEvent.click(screen.getByRole("button", { name: "Send message" }));
	await screen.findByText(/delivery wasn’t confirmed/);
	await userEvent.click(screen.getByRole("button", { name: "Retry message safely" }));
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
		else if (new URL(request.url).pathname.endsWith("/conversation")) {
			conversationReads++;
			body = { controller: "running", latestSequence: 1, turns: [{ id: "turn-1", state: "running", requestedAt: "2026-09-28T00:00:00Z" }], messages: [], activities: conversationReads === 1 ? [{
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
	renderRemoteSession(queryClient);
	const approval = await screen.findByRole("group", { name: "Approval request acp:host:1" });
	expect(screen.queryByRole("combobox", { name: "Message the agent" })).not.toBeInTheDocument();
	fireEvent.click(within(approval).getByRole("button", { name: /Allow once/ }));
	await waitFor(() => expect(decisions).toEqual([{
		url: "http://127.0.0.1:4000/api/v1/sessions/session-1/conversation/approvals/acp%3Ahost%3A1/resolve",
		decisionId: "allow-once",
	}]));
	await waitFor(() => expect(screen.queryByRole("group", { name: "Approval request acp:host:1" })).not.toBeInTheDocument());
	expect(localPost).not.toHaveBeenCalled();
	expect(localGet).not.toHaveBeenCalled();
});
