import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const { localGet, remoteConnect } = vi.hoisted(() => ({ localGet: vi.fn(), remoteConnect: vi.fn() }));
vi.mock("../lib/api-client", () => ({ apiClient: { GET: localGet }, hasTrustedApiBaseUrl: () => true }));
vi.mock("../lib/bridge", () => ({ aoBridge: { remotes: { connect: remoteConnect, disconnect: vi.fn() } } }));
vi.mock("../lib/telemetry", () => ({ captureRendererEvent: vi.fn() }));
vi.mock("../lib/agent-switch-visibility", () => ({ agentSwitchVisibility: { setQueryHealthy: vi.fn() } }));
vi.mock("./useCloudCp", () => ({ useCloudCp: () => ({ ready: false, baseUrl: "", client: {} }) }));
vi.mock("./useCloudOrg", () => ({ useCloudOrg: () => ({ org: undefined, ready: false }) }));

import { connectHost, disconnectHost } from "../lib/host-clients";
import { useRemoteWorkspaces, useWorkspaceQuery, useWorkspaceSession } from "./useWorkspaceQuery";

function wrapper({ children }: { children: ReactNode }) {
	return <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>;
}

afterEach(async () => {
	await disconnectHost("box-a");
	vi.unstubAllGlobals();
});

async function prepareTwoHosts() {
	localGet.mockImplementation(async (path: string) => path === "/api/v1/projects"
		? { data: { projects: [{ id: "project-1", name: "Local", path: "/local" }] } }
		: { data: { sessions: [{ id: "session-1", projectId: "project-1", harness: "codex", status: "working", prs: [] }] } });
	remoteConnect.mockResolvedValue({ hostId: "box-a", label: "Box A", url: "http://box-a:3001", base: "http://127.0.0.1:4000" });
	vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
		const url = input instanceof Request ? input.url : String(input);
		return new Response(JSON.stringify(url.endsWith("/projects")
			? { projects: [{ id: "project-1", name: "Remote", path: "/remote" }] }
			: { sessions: [{ id: "session-1", projectId: "project-1", harness: "codex", status: "working", prs: [] }] }),
		{ status: 200, headers: { "content-type": "application/json" } });
	}));
	await connectHost("http://box-a:3001");
}

it("lists remote projects separately so local actions cannot target a same-named remote session", async () => {
	await prepareTwoHosts();
	const { result } = renderHook(() => ({ local: useWorkspaceQuery(), remote: useRemoteWorkspaces() }), { wrapper });
	await waitFor(() => expect(result.current.remote.data).toHaveLength(1));
	expect(result.current.local.data?.map((project) => project.name)).toEqual(["Local"]);
	expect(result.current.remote.data?.map((project) => [project.name, project.sessions[0]?.hostId])).toEqual([["Remote", "box-a"]]);
});

it("opens the remote session when local and remote use the same session ID", async () => {
	await prepareTwoHosts();
	const { result } = renderHook(() => useWorkspaceSession("session-1", "box-a"), { wrapper });
	await waitFor(() => expect(result.current.data?.workspaceName).toBe("Remote"));
});
