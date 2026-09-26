import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

const remoteConnect = vi.hoisted(() => vi.fn());
vi.mock("../lib/bridge", () => ({ aoBridge: { remotes: { connect: remoteConnect, disconnect: vi.fn() } } }));

import { connectHost, disconnectHost } from "../lib/host-clients";
import { RemoteSpawnSession } from "./RemoteSpawnSession";

afterEach(async () => {
	await disconnectHost("box-b");
	await disconnectHost("box-c");
	vi.unstubAllGlobals();
});

it("starts a worker on Box B, not the local daemon, even when the session ID overlaps", async () => {
	const requests: Array<{ url: string; method: string; body?: unknown }> = [];
	remoteConnect.mockResolvedValue({ hostId: "box-b", label: "Box B", url: "http://box-b:3001", base: "http://127.0.0.1:4400" });
	vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
		const request = input instanceof Request ? input : new Request(input);
		requests.push({ url: request.url, method: request.method, body: request.method === "POST" ? await request.json() : undefined });
		const payload = request.url.endsWith("/agents/readiness/ensure")
			? { agents: [
				{ id: "codex", label: "Codex", effectiveReadiness: "ready" },
				{ id: "claude-code", label: "Claude Code", effectiveReadiness: "not_ready" },
			] }
			: request.url.endsWith("/settings")
				? { chatHarnesses: ["codex"] }
			: request.url.endsWith("/agents/readiness")
				? { agents: [{ id: "codex", label: "Codex", effectiveReadiness: "unknown" }] }
			: request.url.endsWith("/projects")
				? { projects: [{ id: "project-1", name: "Project on Box B", folderMissing: false }] }
				: { session: { id: "same-id" }, promptBytes: 12, systemPromptBytes: 0 };
		return new Response(JSON.stringify(payload), { status: request.url.endsWith("/sessions") ? 201 : 200, headers: { "content-type": "application/json" } });
	}));
	await connectHost("http://box-b:3001");
	const onCreated = vi.fn();
	render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
		<RemoteSpawnSession hostId="box-b" onCreated={onCreated} />
	</QueryClientProvider>);
	await screen.findByRole("option", { name: "Project on Box B" });
	expect(screen.queryByRole("option", { name: "Claude Code" })).not.toBeInTheDocument();
	fireEvent.change(screen.getByRole("combobox", { name: "Project" }), { target: { value: "project-1" } });
	fireEvent.change(screen.getByRole("textbox", { name: "Task" }), { target: { value: "Fix the login test" } });
	fireEvent.click(screen.getByRole("button", { name: "Start on remote host" }));
	await waitFor(() => expect(onCreated).toHaveBeenCalledWith("same-id"));
	expect(requests.map(({ url }) => url).sort()).toEqual([
		"http://127.0.0.1:4400/api/v1/projects",
		"http://127.0.0.1:4400/api/v1/agents/readiness/ensure",
		"http://127.0.0.1:4400/api/v1/settings",
		"http://127.0.0.1:4400/api/v1/sessions",
	].sort());
	expect(requests.find(({ url }) => url.endsWith("/sessions"))).toMatchObject({ method: "POST", body: {
		kind: "worker", projectId: "project-1", harness: "codex", mode: "chat", prompt: "Fix the login test",
	} });
});

it("uses Terminal for a ready agent that cannot run Chat", async () => {
	const requests: Array<{ url: string; body?: unknown }> = [];
	remoteConnect.mockResolvedValue({ hostId: "box-b", label: "Box B", url: "http://box-b:3001", base: "http://127.0.0.1:4400" });
	vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
		const request = input instanceof Request ? input : new Request(input);
		requests.push({ url: request.url, body: request.method === "POST" && request.url.endsWith("/sessions") ? await request.json() : undefined });
		const payload = request.url.endsWith("/agents/readiness/ensure")
			? { agents: [{ id: "unreal-agent", label: "Unreal Agent", effectiveReadiness: "ready" }] }
			: request.url.endsWith("/settings")
				? { chatHarnesses: [] }
				: request.url.endsWith("/projects")
					? { projects: [] }
					: { session: { id: "tui-session" } };
		return new Response(JSON.stringify(payload), { status: request.url.endsWith("/sessions") ? 201 : 200, headers: { "content-type": "application/json" } });
	}));
	await connectHost("http://box-b:3001");
	const onCreated = vi.fn();
	render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
		<RemoteSpawnSession hostId="box-b" onCreated={onCreated} />
	</QueryClientProvider>);
	await screen.findByRole("option", { name: "Unreal Agent" });
	expect(screen.getByRole("combobox", { name: "Interface" })).toHaveValue("tui");
	expect(screen.getByRole("option", { name: "Chat" })).toBeDisabled();
	fireEvent.change(screen.getByRole("textbox", { name: "Task" }), { target: { value: "Inspect logs" } });
	fireEvent.click(screen.getByRole("button", { name: "Start on remote host" }));
	await waitFor(() => expect(onCreated).toHaveBeenCalledWith("tui-session"));
	expect(requests.find(({ url }) => url.endsWith("/sessions"))?.body).toMatchObject({ harness: "unreal-agent", mode: "tui" });
});

it("does not carry Box B's selected project into Box C when both have the same project ID", async () => {
	remoteConnect.mockImplementation(async (url: string) => url.includes("box-b")
		? { hostId: "box-b", label: "Box B", url, base: "http://127.0.0.1:4400" }
		: { hostId: "box-c", label: "Box C", url, base: "http://127.0.0.1:4500" });
	vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
		const request = input instanceof Request ? input : new Request(input);
		const payload = request.url.endsWith("/agents/readiness/ensure")
			? { agents: [{ id: "codex", label: "Codex", effectiveReadiness: "ready" }] }
			: request.url.endsWith("/settings")
				? { chatHarnesses: ["codex"] }
			: { projects: [{ id: "project-1", name: request.url.includes(":4400") ? "Box B project" : "Box C project", folderMissing: false }] };
		return new Response(JSON.stringify(payload), { status: 200, headers: { "content-type": "application/json" } });
	}));
	await connectHost("http://box-b:3001");
	await connectHost("http://box-c:3001");
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const { rerender } = render(<QueryClientProvider client={queryClient}>
		<RemoteSpawnSession hostId="box-b" onCreated={vi.fn()} />
	</QueryClientProvider>);
	await screen.findByRole("option", { name: "Box B project" });
	fireEvent.change(screen.getByRole("combobox", { name: "Project" }), { target: { value: "project-1" } });
	rerender(<QueryClientProvider client={queryClient}>
		<RemoteSpawnSession hostId="box-c" onCreated={vi.fn()} />
	</QueryClientProvider>);
	await screen.findByRole("option", { name: "Box C project" });
	expect(screen.getByRole("combobox", { name: "Project" })).toHaveValue("");
});

it("does not create a session when the selected remote host is disconnected", () => {
	const fetchMock = vi.fn();
	vi.stubGlobal("fetch", fetchMock);
	render(<QueryClientProvider client={new QueryClient()}>
		<RemoteSpawnSession hostId="box-b" onCreated={vi.fn()} />
	</QueryClientProvider>);
	expect(screen.getByRole("button", { name: "Start on remote host" })).toBeDisabled();
	expect(screen.getByRole("alert")).toHaveTextContent("Connect this remote host");
	expect(fetchMock).not.toHaveBeenCalled();
});
