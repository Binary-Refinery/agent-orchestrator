import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { components } from "../../api/schema";
import { InstructorInventory } from "./InstructorInventory";

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));

type Inventory = components["schemas"]["InstructorInventoryResponse"];
type Resource = components["schemas"]["InstructorGitResourceInventory"];
const at = "2026-10-11T10:00:00Z";
const later = "2026-10-11T10:01:00Z";
function resource(patch: Partial<Resource> = {}): Resource {
	return { id: "branch-one", kind: "branch", projectId: "demo", path: "/repo", repositoryPath: "/repo", branch: "feature/forgotten", assignment: "linked", status: "not-observed", dirty: false, firstSeenAt: at, lastSeenAt: at, notObservedSince: later, workIds: ["work-one"], sessionIds: ["worker-one"], ...patch };
}
function inventory(patch: Partial<Inventory> = {}): Inventory {
	return { enabled: true, cached: true, capturedAt: later, nextScanAt: "2026-10-11T10:02:00Z", atomic: false, projects: [], sessions: [], resources: [resource()], unassignedCount: 0, unassignedResourceCount: 0, ...patch };
}

describe("InstructorInventory", () => {
	it("shows retained linked resources and their separate capture and observation times", () => {
		render(<InstructorInventory data={inventory()} loading={false} error={null} />);
		expect(screen.getByText(/Last daemon capture:/)).toBeInTheDocument();
		expect(screen.getByText(/Next daemon capture:/)).toBeInTheDocument();
		expect(screen.getByText(/feature\/forgotten/)).toBeInTheDocument();
		expect(screen.getByText("not-observed")).toBeInTheDocument();
		expect(screen.getByText("work-one")).toBeInTheDocument();
		expect(screen.getByText("worker-one")).toBeInTheDocument();
		expect(screen.getByText(/First seen:.*Last seen:/)).toBeInTheDocument();
		expect(screen.getByText(/Not observed since:/)).toBeInTheDocument();
	});

	it("shows disabled, pending and failed observations without declaring the inventory clean", () => {
		render(<InstructorInventory data={inventory({ enabled: false, cached: false, capturedAt: "0001-01-01T00:00:00Z", nextScanAt: undefined, resources: [], scanError: "Registered repository is unavailable" })} loading={false} error={null} />);
		expect(screen.getByText(/Background inventory is disabled/)).toBeInTheDocument();
		expect(screen.getByText(/first inventory capture/)).toBeInTheDocument();
		expect(screen.getByRole("alert")).toHaveTextContent("Registered repository is unavailable");
		expect(screen.queryByText(/Last daemon capture:/)).not.toBeInTheDocument();
	});

	it("keeps the selected resource and timestamp fixed across a background refetch", async () => {
		const reconcile = vi.fn().mockResolvedValue(undefined);
		const view = render(<InstructorInventory data={inventory()} loading={false} error={null} onReconcile={reconcile} />);
		await userEvent.click(screen.getByRole("button", { name: "Reconcile observation" }));
		expect(reconcile).not.toHaveBeenCalled();
		const dialog = screen.getByRole("dialog");
		expect(within(dialog).getByText(/changes inventory metadata only/)).toBeInTheDocument();
		view.rerender(<InstructorInventory data={inventory({ resources: [resource({ id: "branch-two", branch: "feature/new", lastSeenAt: later })] })} loading={false} error={null} onReconcile={reconcile} />);
		expect(within(dialog).getByText(/feature\/forgotten.*branch-one/)).toBeInTheDocument();
		await userEvent.click(within(dialog).getByRole("button", { name: "Record reconciliation" }));
		expect(reconcile).toHaveBeenCalledExactlyOnceWith("branch-one", at);
	});

	it("preserves a refused reconciliation for review and leaves resource facts visible", async () => {
		const reconcile = vi.fn().mockRejectedValue(new Error("Resource was observed again; refresh inventory"));
		render(<InstructorInventory data={inventory()} loading={false} error={null} onReconcile={reconcile} />);
		await userEvent.click(screen.getByRole("button", { name: "Reconcile observation" }));
		await userEvent.click(screen.getByRole("button", { name: "Record reconciliation" }));
		expect(within(screen.getByRole("dialog")).getByRole("alert")).toHaveTextContent("Resource was observed again");
		expect(screen.getByText("not-observed")).toBeInTheDocument();
	});

	it("never offers reconciliation for observed or already reconciled resources", () => {
		render(<InstructorInventory data={inventory({ resources: [resource({ status: "observed", notObservedSince: undefined }), resource({ id: "history", status: "reconciled", reconciledAt: later })] })} loading={false} error={null} onReconcile={vi.fn()} />);
		expect(screen.queryByRole("button", { name: "Reconcile observation" })).not.toBeInTheDocument();
		expect(screen.getByText(/Reconciled:/)).toBeInTheDocument();
	});

	it("cancels the metadata review without sending a command", async () => {
		const reconcile = vi.fn();
		render(<InstructorInventory data={inventory()} loading={false} error={null} onReconcile={reconcile} />);
		await userEvent.click(screen.getByRole("button", { name: "Reconcile observation" }));
		await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
		expect(reconcile).not.toHaveBeenCalled();
	});
});
