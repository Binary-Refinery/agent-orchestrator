import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { InstructorPolicyDialog } from "./InstructorPolicyDialog";

const mocks = vi.hoisted(() => ({ save: vi.fn(), mapped: true, pinned: vi.fn(), policy: null as Record<string, unknown> | null }));
const policy = () => ({ schema: 1, version: "policy-old", initialPhase: "intake", roles: { worker: { harness: "opencode", model: "provider/model", effort: "maximum", independent: false, effortByModel: mocks.mapped ? { "provider/model": "native-high" } : {} } }, profiles: { push: { command: ["npm", "run", "push-check"], timeoutSeconds: 60 }, full: { command: ["npm", "test"], timeoutSeconds: 120 } }, transitions: [{ from: "review", to: "triage", role: "reviewer", requires: ["review"], humanGate: true }], closeoutRequires: ["push", "full", "review", "delta"], maxFixRounds: 2, nudgeAfterSeconds: 600, escalateAfterSeconds: 1200 });

vi.mock("../hooks/useInstructor", () => ({ instructorQueryKey: ["instructor"], useInstructorPolicy: () => ({ data: mocks.policy, isLoading: false, error: null }), useSaveInstructorPolicy: () => ({ mutateAsync: mocks.save, isPending: false }) }));
vi.mock("../hooks/useAgentReadinessQuery", () => ({ useAgentReadinessQuery: () => ({ data: { agents: [{ id: "opencode", label: "OpenCode" }] } }) }));
vi.mock("../hooks/useAgentModelsQuery", () => ({
	agentModelsQueryOptions: (harness: string) => ({ queryKey: ["models", harness], enabled: Boolean(harness), queryFn: async () => ({ models: [{ id: "provider/model", label: "Model", isDefault: true, efforts: ["native-high", "native-ultra"], effortSupport: "supported" }], allowCustom: false, customModelEntry: "none" }) }),
	refreshAgentModels: vi.fn(),
}));
vi.mock("./settings/AgentModelCombobox", () => ({ AgentModelCombobox: (props: { pinSelection: boolean; tuning: { onEffortChange: (value: string) => void }; }) => { mocks.pinned(props.pinSelection); return <button type="button" onClick={() => props.tuning.onEffortChange("native-ultra")}>Choose native effort</button>; } }));

function renderDialog() {
	return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><InstructorPolicyDialog open onClose={vi.fn()} /></QueryClientProvider>);
}

describe("InstructorPolicyDialog", () => {
	beforeEach(() => { vi.clearAllMocks(); mocks.mapped = true; mocks.policy = policy(); mocks.save.mockImplementation(async (input) => ({ ...input, version: "policy-new" })); });

	it("stores the provider's explicit effort mapping while preserving Human gates and profiles", async () => {
		renderDialog();
		await waitFor(() => expect(screen.getByRole("button", { name: "Save policy" })).toBeEnabled());
		await userEvent.click(screen.getByRole("button", { name: "Choose native effort" }));
		await userEvent.click(screen.getByRole("button", { name: "Save policy" }));
		expect(mocks.save).toHaveBeenCalledWith(expect.objectContaining({ version: "policy-old", roles: { worker: expect.objectContaining({ model: "provider/model", effort: "maximum", effortByModel: { "provider/model": "native-ultra" } }) }, transitions: policy().transitions, profiles: policy().profiles, closeoutRequires: policy().closeoutRequires, maxFixRounds: 2 }));
		expect(mocks.pinned).toHaveBeenCalledWith(true);
	});

	it("does not invent a maximum effort when the selected model has no approved mapping", async () => {
		mocks.mapped = false;
		mocks.policy = policy();
		renderDialog();
		await screen.findByText("Choose a model and its approved native effort before saving.");
		expect(screen.getByRole("button", { name: "Save policy" })).toBeDisabled();
		expect(mocks.save).not.toHaveBeenCalled();
	});

	it.each([
		{ push: null, full: null },
		{ push: [], full: [] },
		{ push: null, full: ["npm", "test"] },
	])("renders and preserves unconfigured profiles on save: %j", async (commands) => {
		const original = { ...policy(), profiles: { push: { command: commands.push, timeoutSeconds: 900 }, full: { command: commands.full, timeoutSeconds: 900 } } };
		mocks.policy = original;
		renderDialog();
		expect(await screen.findByRole("textbox", { name: "push" })).toHaveValue("");
		expect(screen.getByRole("textbox", { name: "full" })).toHaveValue((commands.full ?? []).join("\n"));
		await waitFor(() => expect(screen.getByRole("button", { name: "Save policy" })).toBeEnabled());
		await userEvent.click(screen.getByRole("button", { name: "Save policy" }));
		expect(mocks.save).toHaveBeenCalledExactlyOnceWith(original);
		await screen.findByRole("status");
		expect(screen.getByRole("textbox", { name: "push" })).toHaveValue("");
		expect(mocks.policy).toEqual(original);
	});

	it("clears a configured command to an unconfigured argument array", async () => {
		renderDialog();
		await userEvent.clear(await screen.findByRole("textbox", { name: "push" }));
		await waitFor(() => expect(screen.getByRole("button", { name: "Save policy" })).toBeEnabled());
		await userEvent.click(screen.getByRole("button", { name: "Save policy" }));
		expect(mocks.save).toHaveBeenCalledWith(expect.objectContaining({ profiles: { push: { command: [], timeoutSeconds: 60 }, full: policy().profiles.full } }));
	});

	it("edits a null command without trimming or dropping empty arguments", async () => {
		mocks.policy = { ...policy(), profiles: { ...policy().profiles, push: { command: null, timeoutSeconds: 900 } } };
		renderDialog();
		await userEvent.type(await screen.findByRole("textbox", { name: "push" }), "python\ncheck file.py\n\n--profile\npush");
		await waitFor(() => expect(screen.getByRole("button", { name: "Save policy" })).toBeEnabled());
		await userEvent.click(screen.getByRole("button", { name: "Save policy" }));
		expect(mocks.save).toHaveBeenCalledWith(expect.objectContaining({ profiles: expect.objectContaining({ push: { command: ["python", "check file.py", "", "--profile", "push"], timeoutSeconds: 900 } }) }));
	});

	it("keeps check commands as argument arrays rather than shell expressions", async () => {
		renderDialog();
		const push = await screen.findByRole("textbox", { name: "push" });
		await userEvent.clear(push);
		await userEvent.type(push, "python\ncheck.py\n--profile\npush");
		await userEvent.click(screen.getByRole("button", { name: "Save policy" }));
		expect(mocks.save).toHaveBeenCalledWith(expect.objectContaining({ profiles: expect.objectContaining({ push: { command: ["python", "check.py", "--profile", "push"], timeoutSeconds: 60 } }) }));
	});
});
