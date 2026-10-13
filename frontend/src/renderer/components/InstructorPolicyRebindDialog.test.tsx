import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { InstructorWork } from "../hooks/useInstructor";
import { InstructorPolicyRebindDialog } from "./InstructorPolicyRebindDialog";

const mocks = vi.hoisted(() => ({ preview: vi.fn(), apply: vi.fn(), close: vi.fn(), applied: vi.fn() }));
vi.mock("../hooks/useInstructorRecovery", () => ({ useInstructorPolicyRebindPreview: () => ({ mutateAsync: mocks.preview, isPending: false }), useApplyInstructorPolicyRebind: () => ({ mutateAsync: mocks.apply, isPending: false }) }));
const work = { id: "work-1", name: "Login", projectId: "demo", revision: 7 } as InstructorWork;
const policy = { roles: { orchestrator: { harness: "codex", model: "native-model", effort: "configured", effortByModel: { "native-model": "native-high" } } }, budgets: { maxWorkerLaunches: 3 }, closeoutRequires: ["review", "delta"] };
const exactApproval = "REBIND-GO work-1 target-bound-hash";
const preview = { workId: work.id, workRevision: 7, beforePolicyVersion: "old-policy", targetPolicyVersion: "new-policy", policySource: "project:demo", beforePolicy: policy, effectivePolicy: { ...policy, budgets: { maxWorkerLaunches: 5 } }, invalidatedTransitions: ["review-approved"], supersededExceptionIds: ["exception-1"], historicalEvidenceCount: 4, exactApproval };
function view() { return render(<InstructorPolicyRebindDialog work={work} snapshotId="captured-snapshot" onClose={mocks.close} onApplied={mocks.applied} />); }
async function prepare() { await userEvent.type(screen.getByRole("textbox", { name: "Reason for changing this work" }), "Use reviewed project choices."); await userEvent.click(screen.getByRole("button", { name: "Preview configuration change" })); }

describe("InstructorPolicyRebindDialog", () => {
	beforeEach(() => { vi.clearAllMocks(); mocks.preview.mockResolvedValue(preview); mocks.apply.mockResolvedValue(work); mocks.applied.mockResolvedValue(undefined); });
	it("shows complete governance changes and requires exact approval for the captured target", async () => {
		view(); await prepare();
		expect(screen.getByText("old-policy → new-policy")).toBeInTheDocument();
		expect(screen.getByText(/review-approved/)).toBeInTheDocument(); expect(screen.getByText(/exception-1/)).toBeInTheDocument();
		expect(screen.getByText(/4 historical evidence/)).toBeInTheDocument();
		expect(screen.getByText(/"maxWorkerLaunches": 5/)).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Approve configuration change" })).toBeDisabled();
		await userEvent.type(screen.getByRole("textbox", { name: "Enter the exact approval shown above" }), exactApproval);
		await userEvent.click(screen.getByRole("button", { name: "Approve configuration change" }));
		expect(mocks.apply).toHaveBeenCalledExactlyOnceWith({ workId: "work-1", snapshotId: "captured-snapshot", reason: "Use reviewed project choices.", targetPolicyVersion: "new-policy", approval: exactApproval });
		expect(mocks.applied).toHaveBeenCalledOnce();
	});
	it("invalidates a signed preview when the reason changes", async () => {
		view(); await prepare(); await userEvent.type(screen.getByRole("textbox", { name: "Enter the exact approval shown above" }), exactApproval);
		await userEvent.type(screen.getByRole("textbox", { name: "Reason for changing this work" }), " Updated.");
		expect(screen.queryByRole("button", { name: "Approve configuration change" })).not.toBeInTheDocument(); expect(mocks.apply).not.toHaveBeenCalled();
	});
	it("preserves a rejected proposal and its typed explanation without silently re-previewing", async () => {
		mocks.apply.mockRejectedValue(new Error("Work revision changed. Refresh before reviewing another proposal."));
		view(); await prepare(); await userEvent.type(screen.getByRole("textbox", { name: "Enter the exact approval shown above" }), exactApproval);
		await userEvent.click(screen.getByRole("button", { name: "Approve configuration change" }));
		expect(await screen.findByRole("alert")).toHaveTextContent("Work revision changed"); expect(mocks.preview).toHaveBeenCalledTimes(1);
		expect(screen.getByRole("textbox", { name: "Reason for changing this work" })).toHaveValue("Use reviewed project choices."); expect(mocks.close).not.toHaveBeenCalled();
	});
});
