import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "./ui/tooltip";
import { RemoteProjectBoard } from "./RemoteProjectBoard";

const mocks = vi.hoisted(() => ({
	navigate: vi.fn(),
	post: vi.fn(),
	selectedHosts: [] as string[],
	requestNewTask: vi.fn(),
	openRemoteProjectSettings: vi.fn(),
	connected: ["box-b"],
	workspace: {
		hostId: "box-b", id: "shared", name: "Todo App", path: "/todo",
		orchestratorAgent: "opencode", sessions: [] as Array<Record<string, unknown>>,
	},
}));

vi.mock("@tanstack/react-router", async (importOriginal) => ({
	...await importOriginal<typeof import("@tanstack/react-router")>(),
	useNavigate: () => mocks.navigate,
}));
vi.mock("../hooks/useWorkspaceQuery", () => ({
	remoteWorkspaceQueryKey: (hostId: string) => ["remote-workspaces", hostId],
	useRemoteProjectQuery: () => ({ data: mocks.workspace, isError: false, isSuccess: true }),
}));
vi.mock("../lib/host-clients", () => ({
	clientForHost: (hostId: string) => {
		mocks.selectedHosts.push(hostId);
		return { POST: mocks.post };
	},
	connectedHosts: () => mocks.connected,
	labelForHost: () => "Host B",
	subscribeConnectedHosts: () => () => undefined,
}));
vi.mock("../lib/shell-context", () => ({
	useShell: () => ({ openRemoteProjectSettings: mocks.openRemoteProjectSettings }),
}));
vi.mock("../stores/ui-store", () => ({
	useUiStore: (selector: (state: { requestNewTask: typeof mocks.requestNewTask }) => unknown) =>
		selector({ requestNewTask: mocks.requestNewTask }),
}));

function renderBoard() {
	return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
		<TooltipProvider><RemoteProjectBoard hostId="box-b" projectId="shared" /></TooltipProvider>
	</QueryClientProvider>);
}

beforeEach(() => {
	mocks.navigate.mockReset();
	mocks.post.mockReset();
	mocks.selectedHosts.length = 0;
	mocks.requestNewTask.mockReset();
	mocks.openRemoteProjectSettings.mockReset();
	mocks.workspace.orchestratorAgent = "opencode";
	mocks.workspace.sessions = [];
});

it("starts a new task and orchestrator on the selected host", async () => {
	mocks.post.mockResolvedValue({ data: { orchestrator: { id: "orch-1" } } });
	renderBoard();
	fireEvent.click(screen.getAllByRole("button", { name: /new task/i })[0]);
	expect(mocks.requestNewTask).toHaveBeenCalledWith("shared", "box-b");
	fireEvent.click(screen.getAllByRole("button", { name: "Spawn Orchestrator" })[0]);
	await waitFor(() => expect(mocks.post).toHaveBeenCalledWith("/api/v1/orchestrators", { body: { projectId: "shared" } }));
	await waitFor(() => expect(mocks.navigate).toHaveBeenCalledWith({
		to: "/host/$hostId/project/$projectId/session/$sessionId",
		params: { hostId: "box-b", projectId: "shared", sessionId: "orch-1" },
	}));
});

it("opens a worker with the host ID even when its project ID could exist locally", () => {
	mocks.workspace.sessions = [{
		hostId: "box-b", id: "worker-1", workspaceId: "shared", workspaceName: "Todo App",
		title: "Fix login", provider: "opencode", kind: "worker", status: "working",
		updatedAt: "2026-09-28T00:00:00Z", prs: [],
	}];
	renderBoard();
	fireEvent.click(screen.getByRole("button", { name: "Fix login" }));
	expect(mocks.navigate).toHaveBeenCalledWith({
		to: "/host/$hostId/project/$projectId/session/$sessionId",
		params: { hostId: "box-b", projectId: "shared", sessionId: "worker-1" },
	});
});

it("does not send a spawn when the remote project lacks an orchestrator agent", () => {
	mocks.workspace.orchestratorAgent = "";
	renderBoard();
	fireEvent.click(screen.getAllByRole("button", { name: "Spawn Orchestrator" })[0]);
	expect(mocks.post).not.toHaveBeenCalled();
	expect(mocks.openRemoteProjectSettings).toHaveBeenCalledWith("box-b", "shared");
	expect(screen.getByRole("button", { name: "Configure orchestrator agent" })).toBeVisible();
});

it("offers to replace an orchestrator running the wrong agent", async () => {
	mocks.workspace.sessions = [{
		hostId: "box-b", id: "orch-old", workspaceId: "shared", workspaceName: "Todo App",
		title: "Orchestrator", provider: "codex", kind: "orchestrator", status: "working",
		updatedAt: "2026-09-28T00:00:00Z", prs: [],
	}];
	mocks.post.mockResolvedValue({ data: { orchestrator: { id: "orch-new" } } });
	renderBoard();
	expect(screen.getByText(/Configured orchestrator agent is opencode/)).toBeVisible();
	fireEvent.click(screen.getByRole("button", { name: "Restart" }));
	await waitFor(() => expect(mocks.post).toHaveBeenCalledWith("/api/v1/orchestrators", { body: { projectId: "shared", clean: true } }));
});

it("archives and restores a session only through its remote host", async () => {
	mocks.workspace.sessions = [{
		hostId: "box-b", id: "worker-1", workspaceId: "shared", workspaceName: "Todo App",
		title: "Fix login", provider: "opencode", kind: "worker", status: "working",
		updatedAt: "2026-09-28T00:00:00Z", prs: [],
	}];
	mocks.post.mockResolvedValue({ data: {} });
	const { rerender } = renderBoard();
	fireEvent.click(screen.getByRole("button", { name: "Archive Fix login" }));
	fireEvent.click(screen.getByRole("button", { name: "Confirm, archive session" }));
	await waitFor(() => expect(mocks.post).toHaveBeenCalledWith("/api/v1/sessions/{sessionId}/kill", {
		params: { path: { sessionId: "worker-1" } },
	}));
	mocks.workspace.sessions = [{
		...mocks.workspace.sessions[0], status: "terminated", isTerminated: true,
	}];
	rerender(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
		<TooltipProvider><RemoteProjectBoard hostId="box-b" projectId="shared" /></TooltipProvider>
	</QueryClientProvider>);
	fireEvent.click(screen.getByRole("button", { name: /Archive, 1 session/ }));
	fireEvent.click(await screen.findByRole("button", { name: "Restore Fix login" }));
	await waitFor(() => expect(mocks.post).toHaveBeenCalledWith("/api/v1/sessions/{sessionId}/restore", {
		params: { path: { sessionId: "worker-1" } },
	}));
	expect(mocks.selectedHosts).toEqual(["box-b", "box-b"]);
});
