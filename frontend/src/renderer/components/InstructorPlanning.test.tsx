import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { InstructorPlanning } from "./InstructorPlanning";
import type { InstructorPlanJob } from "../hooks/useInstructor";

const mocks = vi.hoisted(() => ({ jobs: [] as InstructorPlanJob[], create: vi.fn(), refetch: vi.fn() }));
vi.mock("../hooks/useInstructor", () => ({
	useInstructorPlans: () => ({ data: { jobs: mocks.jobs }, isLoading: false, error: null, refetch: mocks.refetch }),
	useCreateInstructorPlan: () => ({ mutateAsync: mocks.create, isPending: false }),
}));
function job(patch: Partial<InstructorPlanJob> = {}): InstructorPlanJob {
	return { id: "plan-1", requestHash: "hash", input: { request: "What should proceed?", snapshotId: "snapshot-1", idempotencyKey: "key" }, policyVersion: "policy-1", requested: { harness: "opencode", model: "native/model", effort: "native-high" }, sent: { harness: "opencode", model: "native/model", effort: "native-high" }, confirmed: { harness: "opencode", model: "native/model", effort: "" }, state: "completed", createdAt: "2026-10-11T10:00:00Z", updatedAt: "2026-10-11T10:01:00Z", output: { summary: "Publication needs the operator.", questions: ["Which release should ship?"], proposals: [{ command: "go", workIds: ["work-1"], reason: "Review the publication gate" }], intake: [{ projectId: "demo", name: "Investigate login", goal: "Find cause", issueId: "42" }] }, ...patch };
}

describe("InstructorPlanning", () => {
	beforeEach(() => { vi.clearAllMocks(); mocks.jobs = []; mocks.create.mockResolvedValue(job({ state: "queued", output: undefined })); mocks.refetch.mockResolvedValue({}); });
	it("queues a bounded strategic request on the displayed snapshot without applying commands", async () => {
		const onProposal = vi.fn(); const onIntake = vi.fn();
		render(<InstructorPlanning snapshotId="snapshot-1" busy={false} onProposal={onProposal} onIntake={onIntake} />);
		await userEvent.type(screen.getByLabelText("Strategic request"), "Continue ready work and explain held gates");
		await userEvent.click(screen.getByRole("button", { name: "Ask Instructor" }));
		expect(mocks.create).toHaveBeenCalledWith({ request: "Continue ready work and explain held gates", snapshotId: "snapshot-1", idempotencyKey: expect.any(String) });
		expect(mocks.refetch).toHaveBeenCalledOnce();
		expect(onProposal).not.toHaveBeenCalled(); expect(onIntake).not.toHaveBeenCalled();
	});
	it("keeps advisory proposals explicit and partial native confirmation unknown", async () => {
		mocks.jobs = [job()]; const onProposal = vi.fn(); const onIntake = vi.fn();
		render(<InstructorPlanning snapshotId="snapshot-1" busy={false} onProposal={onProposal} onIntake={onIntake} />);
		await userEvent.click(screen.getByText("What should proceed?"));
		expect(screen.getByText("Publication needs the operator.")).toBeInTheDocument();
		expect(screen.getByText("opencode · native/model · Unknown")).toBeInTheDocument();
		expect(onProposal).not.toHaveBeenCalled();
		await userEvent.click(screen.getByRole("button", { name: "Apply proposed command" }));
		expect(onProposal).toHaveBeenCalledWith(mocks.jobs[0], mocks.jobs[0].output?.proposals[0]);
		await userEvent.click(screen.getByRole("button", { name: "Review intake draft" }));
		expect(onIntake).toHaveBeenCalledWith(mocks.jobs[0], mocks.jobs[0].output?.intake[0]);
	});
	it("keeps proposals bound to their original snapshot after Status changes", async () => {
		mocks.jobs = [job()];
		render(<InstructorPlanning snapshotId="snapshot-2" busy={false} onProposal={vi.fn()} onIntake={vi.fn()} />);
		await userEvent.click(screen.getByText("What should proceed?"));
		expect(screen.getByRole("button", { name: "Apply proposed command" })).toBeDisabled();
		expect(screen.getByRole("button", { name: "Review intake draft" })).toBeDisabled();
		expect(screen.getByText("This advice refers to an earlier snapshot. Ask again for the current portfolio before applying it.")).toBeInTheDocument();
	});
	it("reports native planning refusal without creating authority or clearing the request", async () => {
		mocks.create.mockRejectedValue(new Error("Instructor model has no approved native effort mapping"));
		render(<InstructorPlanning snapshotId="snapshot-1" busy={false} onProposal={vi.fn()} onIntake={vi.fn()} />);
		await userEvent.type(screen.getByLabelText("Strategic request"), "Assess work");
		await userEvent.click(screen.getByRole("button", { name: "Ask Instructor" }));
		expect(screen.getByRole("alert")).toHaveTextContent("no approved native effort mapping");
		expect(screen.getByLabelText("Strategic request")).toHaveValue("Assess work");
		expect(mocks.create).toHaveBeenCalledOnce();
	});
});
