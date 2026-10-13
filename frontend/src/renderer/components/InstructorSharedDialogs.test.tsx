import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { InstructorNewDialog } from "./InstructorNewDialog";
import { InstructorDeliveryReconcile } from "./InstructorDeliveryReconcile";
import type { InstructorWork } from "../hooks/useInstructor";

const mocks = vi.hoisted(() => ({ reconcile: vi.fn() }));
vi.mock("../hooks/useInstructor", () => ({ useInstructorReconcile: () => ({ mutateAsync: mocks.reconcile, isPending: false }) }));
const uncertain = { id: "work-1", gate: { kind: "delivery_uncertain", dispatchId: "dispatch-one", targetSessionId: "orch-one" } } as InstructorWork;

describe("Instructor shared dialogs", () => {
	beforeEach(() => { vi.clearAllMocks(); mocks.reconcile.mockResolvedValue({}); });
	it("creates a manual work without an issue while preserving its selected project", async () => {
		const submit = vi.fn().mockResolvedValue(undefined);
		render(<InstructorNewDialog draft={{ projectId: "project-one" }} open busy={false} error={null} projects={[{ id: "project-one", name: "Project one" }]} onClose={vi.fn()} onSubmit={submit} />);
		await userEvent.type(screen.getByLabelText("Short work name"), "Manual work");
		await userEvent.type(screen.getByLabelText("Goal"), "A useful finished change");
		await userEvent.click(screen.getByRole("button", { name: "New work" }));
		expect(submit).toHaveBeenCalledWith({ projectId: "project-one", name: "Manual work", goal: "A useful finished change", source: "operator", sourceRef: "" });
	});
	it.each(["snapshot", "dispatch"])("clears a delivery observation when its %s changes", async (field) => {
		const saved = vi.fn().mockResolvedValue(undefined);
		const view = render(<InstructorDeliveryReconcile work={uncertain} snapshotId="snapshot-one" onSaved={saved} />);
		await userEvent.selectOptions(screen.getByLabelText("Observed delivery"), "absent");
		await userEvent.type(screen.getByLabelText("Observation note"), "Checked the exact conversation.");
		expect(screen.getByRole("button", { name: "Record observation" })).toBeEnabled();
		view.rerender(<InstructorDeliveryReconcile work={field === "dispatch" ? { ...uncertain, gate: { ...uncertain.gate!, dispatchId: "dispatch-two" } } : uncertain} snapshotId={field === "snapshot" ? "snapshot-two" : "snapshot-one"} onSaved={saved} />);
		expect(screen.getByLabelText("Observed delivery")).toHaveValue("");
		expect(screen.getByLabelText("Observation note")).toHaveValue("");
		expect(screen.getByRole("button", { name: "Record observation" })).toBeDisabled();
		expect(mocks.reconcile).not.toHaveBeenCalled();
	});
	it("records an explicit observation without replacing a publication approval", async () => {
		const saved = vi.fn().mockResolvedValue(undefined);
		render(<InstructorDeliveryReconcile work={uncertain} snapshotId="snapshot-one" onSaved={saved} />);
		await userEvent.selectOptions(screen.getByLabelText("Observed delivery"), "absent");
		await userEvent.type(screen.getByLabelText("Observation note"), "Checked the exact conversation.");
		await userEvent.click(screen.getByRole("button", { name: "Record observation" }));
		await waitFor(() => expect(saved).toHaveBeenCalledOnce());
		expect(mocks.reconcile).toHaveBeenCalledWith({ snapshotId: "snapshot-one", workId: "work-1", dispatchId: "dispatch-one", accepted: false, note: "Checked the exact conversation." });
	});
});
