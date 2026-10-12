import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { agentModelsQueryKey, type AgentModelCatalog } from "../hooks/useAgentModelsQuery";
import type { InstructorWork } from "../hooks/useInstructor";
import { InstructorExceptionDialog } from "./InstructorExceptionDialog";

const mocks = vi.hoisted(() => ({ preview: vi.fn(), apply: vi.fn(), close: vi.fn(), applied: vi.fn(), catalog: vi.fn() }));
vi.mock("../hooks/useInstructor", () => ({ useInstructorExceptionPreview: () => ({ mutateAsync: mocks.preview, isPending: false }), useApplyInstructorException: () => ({ mutateAsync: mocks.apply, isPending: false }) }));
vi.mock("../hooks/useAgentReadinessQuery", () => ({ useAgentReadinessQuery: () => ({ data: { agents: [{ id: "opencode", label: "OpenCode" }] } }) }));
vi.mock("../lib/api-client", () => ({ apiClient: { GET: mocks.catalog }, apiErrorMessage: () => "Catalog request failed" }));

const at = "2026-10-11T10:00:00Z";
const work: InstructorWork = {
	id: "work-1", projectId: "demo", name: "Login flow", goal: "Make login reliable", source: "issue", sourceRef: "#1", sessionIds: [], launches: [], state: "blocked", phase: "fix", phaseStartedAt: at, phaseSequence: 1, fixPhaseSequence: 1, deltaPhaseSequence: 0, summary: "Round budget reached", next: "Operator decision", policyVersion: "policy-old", evidence: [], fixRounds: 2, openCriticalFindings: 1, revision: 3, createdAt: at, updatedAt: at, progressAt: at, actions: ["details"],
	policy: { schema: 1, version: "policy-old", initialPhase: "intake", roles: { worker: { harness: "opencode", model: "provider/model", effort: "maximum", effortByModel: { "provider/model": "native-high" } } }, profiles: { push: { command: [], timeoutSeconds: 60 } }, transitions: [], closeoutRequires: [], maxFixRounds: 2, nudgeAfterSeconds: 600, escalateAfterSeconds: 1200, attribution: { authorName: "Operator", authorEmail: "operator@example.invalid", coAuthorName: "Agent", coAuthorEmail: "agent@example.invalid", sessionTrailer: "AO-Session", issueTrailer: "Issue" }, traceability: { required: true, requireAssignee: true, projectNumber: 0, requiredPhases: [], allowedRepositories: ["owner/repo"], allowedBaseBranches: ["main"] } },
};

function catalog(label: string, efforts: string[]): AgentModelCatalog {
	return { agentId: "opencode", allowCustom: false, customModelEntry: "none", fetchedAt: at, selectionMode: "catalog", source: "project-config", stale: false, models: [{ id: "provider/model", label, efforts, effortSupport: "supported" }] };
}

const projectCatalog = catalog("Project model", ["native-high", "native-ultra"]);
const otherProjectCatalog = catalog("Other project model", ["other-native"]);
const globalCatalog = catalog("Global model", ["global-native"]);

function view(selectedWork = work, queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } }), extras: Pick<React.ComponentProps<typeof InstructorExceptionDialog>, "budgetUsage" | "clickApproval" | "observedWork"> = {}) {
	const dialog = (currentWork: InstructorWork, currentExtras = extras, currentSnapshotId = "snapshot-frozen") => <QueryClientProvider client={queryClient}><InstructorExceptionDialog open work={currentWork} snapshotId={currentSnapshotId} {...currentExtras} onClose={mocks.close} onApplied={mocks.applied} /></QueryClientProvider>;
	const rendered = render(dialog(selectedWork));
	return { ...rendered, queryClient, rerenderWork: (currentWork: InstructorWork) => rendered.rerender(dialog(currentWork)), rerenderFacts: (currentExtras: typeof extras, snapshotId = "snapshot-frozen") => rendered.rerender(dialog(selectedWork, currentExtras, snapshotId)) };
}

async function chooseNativeEffort(label: string, effort: string) {
	await userEvent.click(screen.getByRole("button", { name: "Requested model / effort" }));
	expect(screen.queryByRole("searchbox")).not.toBeInTheDocument();
	expect(screen.queryByRole("menuitem", { name: /custom model|Use agent model/ })).not.toBeInTheDocument();
	await userEvent.click(screen.getByRole("menuitem", { name: `${label} provider` }));
	await userEvent.click(await screen.findByRole("menuitemradio", { name: effort }));
}

async function prepareBudget() {
	await userEvent.type(screen.getByRole("spinbutton"), "4");
	await userEvent.type(screen.getByRole("textbox", { name: "Reason for this exception" }), "A bounded extra pass can resolve the remaining finding.");
	await userEvent.click(screen.getByRole("button", { name: "Preview exception" }));
}

describe("InstructorExceptionDialog", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mocks.catalog.mockImplementation(async (_path: string, options: { params: { query: { projectId?: string } } }) => ({ data: options.params.query.projectId === "demo" ? projectCatalog : options.params.query.projectId === "other-project" ? otherProjectCatalog : globalCatalog }));
		mocks.preview.mockResolvedValue({ workId: work.id, policyVersion: work.policyVersion, proposalHash: "proposal-exact", exactApproval: "EXCEPTION-GO work-1 proposal-exact", effectivePolicy: { ...work.policy, maxFixRounds: 4 } }); mocks.apply.mockResolvedValue(work); mocks.applied.mockResolvedValue(undefined);
	});

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

	it("loads the Work project catalog, invalidates edited previews and sends its native choice without custom entry", async () => {
		view(); await prepareBudget();
		await userEvent.click(screen.getByRole("checkbox", { name: "Override Worker or Reviewer selection" }));
		await waitFor(() => expect(mocks.catalog).toHaveBeenCalledWith("/api/v1/agents/{agent}/models", { params: { path: { agent: "opencode" }, query: { projectId: "demo" } } }));
		await chooseNativeEffort("Project model", "Native-ultra");
		expect(screen.queryByRole("textbox", { name: "Type the exact exception approval above" })).not.toBeInTheDocument();
		await waitFor(() => expect(screen.getByRole("button", { name: "Preview exception" })).toBeEnabled());
		await userEvent.click(screen.getByRole("button", { name: "Preview exception" }));
		expect(mocks.preview.mock.calls[1][0]).toMatchObject({ role: "worker", selection: { harness: "opencode", model: "provider/model", effort: "native-ultra" } });
		expect(mocks.apply).not.toHaveBeenCalled();
	});

	it("keeps project catalogs and native effort admission separate when the Work project changes", async () => {
		const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		queryClient.setQueryData(agentModelsQueryKey("opencode", ""), globalCatalog);
		const rendered = view(work, queryClient);
		await userEvent.click(screen.getByRole("checkbox", { name: "Override Worker or Reviewer selection" }));
		await waitFor(() => expect(queryClient.getQueryData(agentModelsQueryKey("opencode", "demo"))).toEqual(projectCatalog));
		await chooseNativeEffort("Project model", "Native-ultra");

		const otherWork = { ...work, id: "work-2", projectId: "other-project" };
		rendered.rerenderWork(otherWork);
		await userEvent.click(screen.getByRole("checkbox", { name: "Override Worker or Reviewer selection" }));
		await waitFor(() => expect(mocks.catalog).toHaveBeenCalledWith("/api/v1/agents/{agent}/models", { params: { path: { agent: "opencode" }, query: { projectId: "other-project" } } }));
		await waitFor(() => expect(queryClient.getQueryData(agentModelsQueryKey("opencode", "other-project"))).toEqual(otherProjectCatalog));
		expect(queryClient.getQueryData(agentModelsQueryKey("opencode", "demo"))).toEqual(projectCatalog);
		expect(queryClient.getQueryData(agentModelsQueryKey("opencode", ""))).toEqual(globalCatalog);
		await userEvent.type(screen.getByRole("textbox", { name: "Reason for this exception" }), "Use this project's native variant.");
		expect(screen.getByRole("button", { name: "Preview exception" })).toBeDisabled();
		expect(mocks.preview).not.toHaveBeenCalled();
		await chooseNativeEffort("Other project model", "Other-native");
		await waitFor(() => expect(screen.getByRole("button", { name: "Preview exception" })).toBeEnabled());
		await userEvent.click(screen.getByRole("button", { name: "Preview exception" }));
		expect(mocks.preview).toHaveBeenCalledWith(expect.objectContaining({ workId: "work-2", role: "worker", selection: { harness: "opencode", model: "provider/model", effort: "other-native" } }));
		expect(mocks.apply).not.toHaveBeenCalled();
	});

	it("shows a server refusal without converting it into an exception grant", async () => {
		mocks.preview.mockRejectedValueOnce(new Error("Snapshot revision changed; refresh Status"));
		view(); await prepareBudget();
		expect(screen.getByRole("alert")).toHaveTextContent("Snapshot revision changed; refresh Status");
		expect(mocks.apply).not.toHaveBeenCalled();
	});
	it("shows measured work budgets and signs the exact scoped proposal by click", async () => {
		const limits = { maxConcurrentSessions: 8, maxSessions: 32, maxTurns: 256, maxDelegations: 24 };
		const selectedWork = { ...work, policy: { ...work.policy, budgets: limits } };
		mocks.preview.mockResolvedValue({ workId: work.id, policyVersion: work.policyVersion, proposalHash: "proposal-exact", exactApproval: "EXCEPTION-GO work-1 proposal-exact", effectivePolicy: { ...selectedWork.policy, budgets: { ...limits, maxTurns: 512 } } });
		view(selectedWork, undefined, { clickApproval: true, budgetUsage: { concurrentSessions: 6, sessions: 29, turns: 250, delegations: 20 } });
		expect(screen.getByText("6 / 8")).toBeInTheDocument();
		expect(screen.getByText("29 / 32")).toBeInTheDocument();
		expect(screen.getByText("250 / 256")).toBeInTheDocument();
		expect(screen.getByText("20 / 24")).toBeInTheDocument();
		await userEvent.click(screen.getByRole("checkbox", { name: "Adjust this work’s execution budgets" }));
		const nativeTurns = screen.getByRole("spinbutton", { name: "Native turns" });
		await userEvent.clear(nativeTurns); await userEvent.type(nativeTurns, "512");
		await userEvent.type(screen.getByRole("textbox", { name: "Reason for this exception" }), "Authorize only the needed turns for this work.");
		await userEvent.click(screen.getByRole("button", { name: "Preview exception" }));
		expect(mocks.preview).toHaveBeenCalledWith(expect.objectContaining({ snapshotId: "snapshot-frozen", workId: "work-1", executionBudgets: { ...limits, maxTurns: 512 } }));
		expect(screen.getByText("512")).toBeInTheDocument();
		expect(screen.queryByRole("textbox", { name: "Type the exact exception approval above" })).not.toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Grant this work exception" })).toBeDisabled();
		await userEvent.click(screen.getByRole("checkbox", { name: "I approve this captured work exception and the displayed model or budget changes." }));
		await userEvent.click(screen.getByRole("button", { name: "Grant this work exception" }));
		expect(mocks.apply).toHaveBeenCalledWith({ ...mocks.preview.mock.calls[0][0], approval: "EXCEPTION-GO work-1 proposal-exact" });
	});

	it("invalidates a signed budget preview after editing and does not admit fractional caps", async () => {
		view(work, undefined, { clickApproval: true });
		await userEvent.click(screen.getByRole("checkbox", { name: "Adjust this work’s execution budgets" }));
		const nativeTurns = screen.getByRole("spinbutton", { name: "Native turns" });
		await userEvent.clear(nativeTurns); await userEvent.type(nativeTurns, "400");
		await userEvent.type(screen.getByRole("textbox", { name: "Reason for this exception" }), "Increase the work turn cap.");
		await userEvent.click(screen.getByRole("button", { name: "Preview exception" }));
		await userEvent.click(screen.getByRole("checkbox", { name: "I approve this captured work exception and the displayed model or budget changes." }));
		await userEvent.clear(nativeTurns); await userEvent.type(nativeTurns, "2.5");
		expect(screen.queryByRole("checkbox", { name: "I approve this captured work exception and the displayed model or budget changes." })).not.toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Preview exception" })).toBeDisabled();
		expect(mocks.apply).not.toHaveBeenCalled();
	});

	it.each(["concurrentSessions", "sessions", "turns", "delegations"] as const)("invalidates a clicked signature when measured %s changes without silently changing its scope", async (counter) => {
		const usage = { concurrentSessions: 1, sessions: 2, turns: 3, delegations: 4 };
		const rendered = view(work, undefined, { clickApproval: true, budgetUsage: usage });
		await prepareBudget();
		await userEvent.click(screen.getByRole("checkbox", { name: "I approve this captured work exception and the displayed model or budget changes." }));
		expect(screen.getByRole("button", { name: "Grant this work exception" })).toBeEnabled();
		rendered.rerenderFacts({ clickApproval: true, budgetUsage: { ...usage, [counter]: usage[counter] + 1 } }, "snapshot-live-new");
		expect(screen.queryByRole("button", { name: "Grant this work exception" })).not.toBeInTheDocument();
		expect(screen.getByRole("alert")).toHaveTextContent("Work facts changed. Preview the proposal again before signing.");
		await userEvent.click(screen.getByRole("button", { name: "Preview exception" }));
		expect(mocks.preview.mock.calls[1][0].snapshotId).toBe("snapshot-frozen");
		expect(screen.getByRole("checkbox", { name: "I approve this captured work exception and the displayed model or budget changes." })).not.toBeChecked();
		expect(mocks.apply).not.toHaveBeenCalled();
	});

	it.each(["revision", "policyVersion"] as const)("invalidates a clicked signature on observed %s changes and keeps the original scope", async (field) => {
		const rendered = view(work, undefined, { clickApproval: true, observedWork: { revision: work.revision, policyVersion: work.policyVersion } });
		await prepareBudget();
		await userEvent.click(screen.getByRole("checkbox", { name: "I approve this captured work exception and the displayed model or budget changes." }));
		rendered.rerenderFacts({ clickApproval: true, observedWork: { revision: field === "revision" ? work.revision + 1 : work.revision, policyVersion: field === "policyVersion" ? "policy-updated" : work.policyVersion } }, "snapshot-live-new");
		expect(screen.queryByRole("button", { name: "Grant this work exception" })).not.toBeInTheDocument();
		expect(screen.getByText(/The captured work revision or policy changed/)).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Preview exception" })).toBeDisabled();
		expect(screen.getByText(/snapshot-frozen/)).toBeInTheDocument();
		expect(screen.queryByText(/snapshot-live-new/)).not.toBeInTheDocument();
		expect(mocks.apply).not.toHaveBeenCalled();
	});

	it("keeps a valid signed preview during a poll with a new snapshot and unchanged facts", async () => {
		const usage = { concurrentSessions: 1, sessions: 2, turns: 3, delegations: 4 };
		const rendered = view(work, undefined, { clickApproval: true, budgetUsage: usage });
		await prepareBudget();
		await userEvent.click(screen.getByRole("checkbox", { name: "I approve this captured work exception and the displayed model or budget changes." }));
		rendered.rerenderFacts({ clickApproval: true, budgetUsage: { ...usage }, observedWork: { revision: work.revision, policyVersion: work.policyVersion } }, "snapshot-live-new");
		expect(screen.getByRole("checkbox", { name: "I approve this captured work exception and the displayed model or budget changes." })).toBeChecked();
		await userEvent.click(screen.getByRole("button", { name: "Grant this work exception" }));
		expect(mocks.apply).toHaveBeenCalledWith(expect.objectContaining({ snapshotId: "snapshot-frozen", workId: work.id, approval: "EXCEPTION-GO work-1 proposal-exact" }));
	});

	it("does not reintroduce a late preview after counters change during the request", async () => {
		let complete!: (value: Record<string, unknown>) => void;
		mocks.preview.mockImplementationOnce(() => new Promise((resolve) => { complete = resolve; }));
		const usage = { concurrentSessions: 1, sessions: 2, turns: 3, delegations: 4 };
		const rendered = view(work, undefined, { clickApproval: true, budgetUsage: usage });
		await prepareBudget();
		rendered.rerenderFacts({ clickApproval: true, budgetUsage: { ...usage, turns: 4 } });
		await act(async () => { complete({ workId: work.id, policyVersion: work.policyVersion, proposalHash: "proposal-exact", exactApproval: "EXCEPTION-GO work-1 proposal-exact", effectivePolicy: { ...work.policy, maxFixRounds: 4 } }); });
		expect(screen.queryByRole("button", { name: "Grant this work exception" })).not.toBeInTheDocument();
		expect(screen.getByRole("alert")).toHaveTextContent("Work facts changed. Preview the proposal again before signing.");
		expect(mocks.apply).not.toHaveBeenCalled();
	});

	it("keeps the original policy and proposal inputs when the legacy parent receives a changed work", async () => {
		const rendered = view();
		await prepareBudget();
		await userEvent.type(screen.getByRole("textbox", { name: "Type the exact exception approval above" }), "EXCEPTION-GO work-1 proposal-exact");
		rendered.rerenderWork({ ...work, revision: work.revision + 1, policyVersion: "policy-new", policy: { ...work.policy, maxFixRounds: 8 } });
		expect(screen.getByText("Higher fix-round budget (current: 2, optional)")).toBeInTheDocument();
		expect(screen.queryByText("Higher fix-round budget (current: 8, optional)")).not.toBeInTheDocument();
		expect(screen.getByRole("spinbutton")).toHaveValue(4);
		expect(screen.getByRole("textbox", { name: "Reason for this exception" })).toHaveValue("A bounded extra pass can resolve the remaining finding.");
		expect(screen.getByRole("button", { name: "Preview exception" })).toBeDisabled();
		expect(screen.queryByRole("button", { name: "Grant this work exception" })).not.toBeInTheDocument();
	});

});
