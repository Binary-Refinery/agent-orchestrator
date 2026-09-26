import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { RemoteHostsSection } from "./RemoteHostsSection";

it("keeps offline hosts visible and opens a same-ID session on the selected host", () => {
	const open = vi.fn();
	const retry = vi.fn();
	const start = vi.fn();
	render(<RemoteHostsSection
		hosts={[
			{ hostId: "box-a", label: "Box A", url: "http://box-a:3001", status: "offline" },
			{ hostId: "box-b", label: "Box B", url: "http://box-b:3001", status: "connected" },
		]}
		workspaces={[{
			hostId: "box-a", id: "project-a", name: "Old project", path: "/stale", sessions: [{
				hostId: "box-a", id: "old-session", workspaceId: "project-a", workspaceName: "Old project",
				title: "Stale session", provider: "codex", status: "working", updatedAt: "2026-01-01T00:00:00Z", prs: [],
			}],
		}, {
			hostId: "box-b", id: "project-1", name: "Agent Repo", path: "/remote", sessions: [{
				hostId: "box-b", id: "session-1", workspaceId: "project-1", workspaceName: "Agent Repo",
				title: "Fix login", provider: "codex", status: "working", updatedAt: "2026-01-01T00:00:00Z", prs: [],
			}],
		}]}
		onOpenSession={open}
		onStart={start}
		onRetry={retry}
	/>);
	expect(screen.getByText("Box A")).toBeVisible();
	expect(screen.queryByText("Stale session")).not.toBeInTheDocument();
	fireEvent.click(screen.getByRole("button", { name: "Retry Box A" }));
	expect(retry).toHaveBeenCalledOnce();
	fireEvent.click(screen.getByRole("button", { name: "Start on Box B" }));
	expect(start).toHaveBeenCalledWith("box-b");
	fireEvent.click(screen.getByRole("button", { name: "Fix login" }));
	expect(open).toHaveBeenCalledWith("box-b", "project-1", "session-1");
});

it("shows a retry action when a connected host cannot load its sessions", () => {
	const retry = vi.fn();
	render(<RemoteHostsSection
		hosts={[{ hostId: "box-a", label: "Box A", url: "http://box-a:3001", status: "connected" }]}
		workspaces={[]}
		failedHostIds={["box-a"]}
		onOpenSession={vi.fn()}
		onStart={vi.fn()}
		onRetry={retry}
	/>);
	fireEvent.click(screen.getByRole("button", { name: "Could not load sessions. Retry" }));
	expect(retry).toHaveBeenCalledOnce();
});
