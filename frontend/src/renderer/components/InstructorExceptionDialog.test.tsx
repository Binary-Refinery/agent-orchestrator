import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { InstructorWork } from "../hooks/useInstructor";
import { InstructorExceptionDialog } from "./InstructorExceptionDialog";

const mocks = vi.hoisted(() => ({ preview: vi.fn(), apply: vi.fn(), close: vi.fn(), applied: vi.fn() }));
vi.mock("../hooks/useInstructor", () => ({ useInstructorExceptionPreview: () => ({ mutateAsync: mocks.preview, isPending: false }), useApplyInstructorException: () => ({ mutateAsync: mocks.apply, isPending: false }) }));
vi.mock("../hooks/useAgentReadinessQuery", () => ({ useAgentReadinessQuery: () => ({ data: { agents: [{ id: "opencode", label: "OpenCode" }] } }) }));
vi.mock("../hooks/useAgentModelsQuery", () => ({ agentModelsQueryOptions: (harness: string) => ({ queryKey: ["models", harness], enabled: Boolean(harness), queryFn: async () => ({ models: [{ id: "provider/model", efforts: ["native-high", "native-ultra"] }] }) }) }));
vi.mock("./settings/AgentModelCombobox", () => ({ AgentModelCombobox: ({ tuning }: { tuning: { onEffortChange: (value: string) => void } }) => <button type="button" onClick={() => tuning.onEffortChange("native-ultra")}>Choose native effort</button> }));

const at = "2026-10-11T10:00:00Z";
const work: InstructorWork = {
	id: "work-1", projectId: "demo", name: "Login flow", goal: "Make login reliable", source: "issue", sourceRef: "#1", sessionIds: [], launches: [], state: "blocked", phase: "fix", phaseStartedAt: at, phaseSequence: 1, fixPhaseSequence: 1, deltaPhaseSequence: 0, summary: "Round budget reached", next: "Operator decision", policyVersion: "policy-old", evidence: [], fixRounds: 2, openCriticalFindings: 1, revision: 3, createdAt: at, updatedAt: at, progressAt: at, actions: ["details"],
	policy: { schema: 1, version: "policy-old", initialPhase: "intake", roles: { worker: { harness: "opencode", model: "provider/model", effort: "maximum", effortByModel: { "provider/model": "native-high" } } }, profiles: { push: { command: [], timeoutSeconds: 60 } }, transitions: [], closeoutRequires: [], maxFixRounds: 2, nudgeAfterSeconds: 600, escalateAfterSeconds: 1200, attribution: { authorName: "Operator", authorEmail: "operator@example.invalid", coAuthorName: "Agent", coAuthorEmail: "agent@example.invalid", sessionTrailer: "AO-Session", issueTrailer: "Issue" }, traceability: { required: true, requireAssignee: true, projectNumber: 0, requiredPhases: [], allowedRepositories: ["owner/repo"], allowedBaseBranches: ["main"] } },
};

function view() {
	return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><InstructorExceptionDialog open work={work} snapshotId="snapshot-frozen" onClose={mocks.close} onApplied={mocks.applied} /></QueryClientProvider>);
}

async function prepareBudget() {
	await userEvent.type(screen.getByRole("spinbutton"), "4");
	await userEvent.type(screen.getByRole("textbox", { name: "Reason for this exception" }), "A bounded extra pass can resolve the remaining finding.");
	await userEvent.click(screen.getByRole("button", { name: "Preview exception" }));
}

describe("InstructorExceptionDialog", () => {
	beforeEach(() => { vi.clearAllMocks(); mocks.preview.mockResolvedValue({ workId: work.id, policyVersion: work.policyVersion, proposalHash: "proposal-exact", exactApproval: "EXCEPTION-GO work-1 proposal-exact", effectivePolicy: { ...work.policy, maxFixRounds: 4 } }); mocks.apply.mockResolvedValue(work); mocks.applied.mockResolvedValue(undefined); });

	it("previews a frozen bounded proposal and requires manual exact approval before granting", async () => {
		view(); await prepareBudget();
		expect(mocks.preview).toHaveBeenCalledWith(expect.objectContaining({ snapshotId: "snapshot-frozen", workId: "work-1", maxFixRounds: 4, expiresAt: expect.any(String) }));
		expect(mocks.apply).not.toHaveBeenCalled();
		const approval = screen.getByRole("textbox", { name: "Type the exact exception approval above" });
		expect(approval).toHaveValue("");
		expect(screen.getByRole("button", { name: "Grant this work exception" })).toBeDisabled();
		await userEvent.type(approval, "EXCEPTION-GO work-1 proposal-exact");
		await userEvent.click(screen.getByRole("button", { name: "Grant this work exception" }));
		expect(mocks.apply).toHaveBeenCalledWith({ ...mocks.preview.mock.calls[0][0], approval: "EXCEPTION-GO work-1 proposal-exact" });
		expect(mocks.applied).toHaveBeenCalledOnce();
	});

	it("invalidates the preview after edits and sends the explicit provider-native choice", async () => {
		view(); await prepareBudget();
		await userEvent.click(screen.getByRole("checkbox", { name: "Override Worker or Reviewer selection" }));
		await userEvent.click(screen.getByRole("button", { name: "Choose native effort" }));
		expect(screen.queryByRole("textbox", { name: "Type the exact exception approval above" })).not.toBeInTheDocument();
		await waitFor(() => expect(screen.getByRole("button", { name: "Preview exception" })).toBeEnabled());
		await userEvent.click(screen.getByRole("button", { name: "Preview exception" }));
		expect(mocks.preview.mock.calls[1][0]).toMatchObject({ role: "worker", selection: { harness: "opencode", model: "provider/model", effort: "native-ultra" } });
		expect(mocks.apply).not.toHaveBeenCalled();
	});

	it("shows a server refusal without converting it into an exception grant", async () => {
		mocks.preview.mockRejectedValueOnce(new Error("Snapshot revision changed; refresh Status"));
		view(); await prepareBudget();
		expect(screen.getByRole("alert")).toHaveTextContent("Snapshot revision changed; refresh Status");
		expect(mocks.apply).not.toHaveBeenCalled();
	});
});
