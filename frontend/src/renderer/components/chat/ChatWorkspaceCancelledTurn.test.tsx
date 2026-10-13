import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { chatFixtureEmpty } from "../../lib/chat-fixture";
import type { ConversationSnapshot } from "../../types/conversation";
import { TooltipProvider } from "../ui/tooltip";
import { ChatWorkspace } from "./ChatWorkspace";

const prompt = "Implement the reviewed worker task";
const rejection = "governed directive no longer admitted: worker launch belongs to a stale phase";

function cancelledSnapshot(reason?: string, providerTurnId?: string): ConversationSnapshot {
	const at = "2026-10-13T13:00:00Z";
	return {
		...chatFixtureEmpty,
		latestSequence: 1,
		turns: [{ id: "blocked-task", state: "cancelled", requestedAt: at, completedAt: at, errorMessage: reason, providerTurnId }],
		items: [{ kind: "message", id: "task-prompt", turnId: "blocked-task", sequence: 1, revision: 0, role: "user", origin: "daemon", text: prompt, streaming: false, delivery: "accepted", createdAt: at }],
	};
}

function workspace(snapshot: ConversationSnapshot, retry = vi.fn()) {
	return <TooltipProvider><ChatWorkspace snapshot={snapshot} retryControl={{ retry }} /></TooltipProvider>;
}

describe("ChatWorkspace rejected task visibility", () => {
	it.each([rejection, "native-turn budget cap reached for this work"])("keeps the unsent task and durable cause visible without retry: %s", (reason) => {
		const retry = vi.fn();
		render(workspace(cancelledSnapshot(reason), retry));

		expect(screen.getByText(prompt)).toBeVisible();
		expect(screen.getByText("Message blocked before delivery")).toBeVisible();
		expect(screen.getByText(reason)).toBeVisible();
		expect(screen.queryByText("The agent ran into a problem")).not.toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Retry this turn" })).not.toBeInTheDocument();
		expect(retry).not.toHaveBeenCalled();
	});

	it.each([undefined, "  "])("keeps deliberate queue cancellation quiet when no cause is recorded", (reason) => {
		const snapshot = cancelledSnapshot(reason);
		snapshot.items = snapshot.items.map((item) => item.kind === "message" ? { ...item, origin: "human" } : item);
		render(workspace(snapshot));

		expect(screen.queryByText(prompt)).not.toBeInTheDocument();
		expect(screen.queryByText("Message blocked before delivery")).not.toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Retry this turn" })).not.toBeInTheDocument();
	});

	it("does not claim an already delivered cancellation was blocked before delivery", () => {
		render(workspace(cancelledSnapshot("interrupted after native admission", "native-turn")));
		expect(screen.queryByText("Message blocked before delivery")).not.toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Retry this turn" })).not.toBeInTheDocument();
	});

	it("reveals a rejected queued task when the durable snapshot refreshes", () => {
		const cancelled = cancelledSnapshot(rejection);
		const queued: ConversationSnapshot = { ...cancelled, turns: cancelled.turns.map((turn) => ({ ...turn, state: "queued", errorMessage: undefined, completedAt: undefined })) };
		const view = render(workspace(queued));
		expect(screen.queryByText(rejection)).not.toBeInTheDocument();

		view.rerender(workspace(cancelled));
		expect(screen.getByText(prompt)).toBeVisible();
		expect(screen.getByText(rejection)).toBeVisible();
		view.rerender(workspace(cancelledSnapshot("governed directive no longer admitted: role requires codex, got opencode")));
		expect(screen.queryByText(rejection)).not.toBeInTheDocument();
		expect(screen.getByText("governed directive no longer admitted: role requires codex, got opencode")).toBeVisible();
		expect(screen.queryByRole("button", { name: "Retry this turn" })).not.toBeInTheDocument();
	});
});
