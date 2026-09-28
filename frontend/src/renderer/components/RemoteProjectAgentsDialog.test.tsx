import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { RemoteProjectAgentsDialog } from "./RemoteProjectAgentsDialog";

const host = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }));

vi.mock("../lib/host-clients", () => ({ clientForHost: () => ({ GET: host.get, POST: host.post, PUT: host.put }) }));

beforeEach(() => {
	host.get.mockReset();
	host.post.mockReset();
	host.put.mockReset();
	host.get.mockResolvedValue({ data: { status: "ok", project: { id: "project-a", name: "ai-learning-aid", config: { defaultBranch: "main" } } } });
	host.post.mockImplementation(async (path: string) => path === "/api/v1/agents/readiness/ensure"
		? { data: { agents: [{ id: "opencode", label: "OpenCode", effectiveReadiness: "ready" }] } }
		: { data: { orchestrator: { id: "orch-a" } } });
	host.put.mockResolvedValue({ data: { project: { id: "project-a" } } });
});

function renderDialog() {
	const onSaved = vi.fn();
	const onOpenChange = vi.fn();
	render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
		<RemoteProjectAgentsDialog hostId="box-a" hostLabel="Host A" projectId="project-a" connected onSaved={onSaved} onOpenChange={onOpenChange} />
	</QueryClientProvider>);
	return { onSaved, onOpenChange };
}

it("configures an existing project on its host and starts its orchestrator", async () => {
	const user = userEvent.setup();
	const { onOpenChange } = renderDialog();
	expect(await screen.findByRole("dialog", { name: "Project settings · ai-learning-aid" })).toBeVisible();
	await user.click(await screen.findByRole("button", { name: "Save changes" }));
	await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
	expect(host.put).toHaveBeenCalledWith("/api/v1/projects/{id}/config", {
		params: { path: { id: "project-a" } },
		body: { config: { defaultBranch: "main", worker: { agent: "opencode" }, orchestrator: { agent: "opencode" } } },
	});
	// A legacy project may already have an active orchestrator despite missing config.
	expect(host.post).toHaveBeenCalledWith("/api/v1/orchestrators", { body: { projectId: "project-a", clean: true } });
});

it("retries orchestrator startup without saving the config again", async () => {
	const user = userEvent.setup();
	host.post.mockImplementation(async (path: string) => {
		if (path === "/api/v1/agents/readiness/ensure") return { data: { agents: [{ id: "opencode", label: "OpenCode", effectiveReadiness: "ready" }] } };
		return host.post.mock.calls.filter(([calledPath]) => calledPath === "/api/v1/orchestrators").length === 1
			? { error: { message: "startup failed" } }
			: { data: { orchestrator: { id: "orch-a" } } };
	});
	const { onOpenChange } = renderDialog();
	await user.click(await screen.findByRole("button", { name: "Save changes" }));
	expect(await screen.findByRole("alert")).toHaveTextContent("startup failed");
	await user.click(screen.getByRole("button", { name: "Retry" }));
	await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
	expect(host.put).toHaveBeenCalledTimes(1);
});
