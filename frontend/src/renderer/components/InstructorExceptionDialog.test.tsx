import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
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

function view(selectedWork = work, queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
	const dialog = (currentWork: InstructorWork) => <QueryClientProvider client={queryClient}><InstructorExceptionDialog open work={currentWork} snapshotId="snapshot-frozen" onClose={mocks.close} onApplied={mocks.applied} /></QueryClientProvider>;
	const rendered = render(dialog(selectedWork));
	return { ...rendered, queryClient, rerenderWork: (currentWork: InstructorWork) => rendered.rerender(dialog(currentWork)) };
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
});
