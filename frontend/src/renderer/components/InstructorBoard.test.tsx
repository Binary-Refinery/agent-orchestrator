import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { InstructorBoard } from "./InstructorBoard";

const mocks = vi.hoisted(() => ({
	enabled: true, publication: { available: true, reason: "" }, snapshot: {} as Record<string, unknown>,
	command: vi.fn(), refetch: vi.fn(), inventoryRefetch: vi.fn(), navigate: vi.fn(), projectSettings: vi.fn(),
	membersRead: vi.fn(), members: null as Record<string, unknown> | null,
	details: null as Record<string, unknown> | null,
}));
const commands = [
	{ action: "weiter", name: "Weiter", mutates: true, snapshotRequired: true },
	{ action: "go", name: "Go", mutates: true, snapshotRequired: true },
	{ action: "pruefen", name: "Prüfen", mutates: true, snapshotRequired: true },
];
function work(patch: Record<string, unknown> = {}) {
	return { id: "work-1", projectId: "project-a", name: "Login", goal: "Reliable login", sourceRef: "#123", phase: "implementation", state: "ready", next: "Build login", policyVersion: "policy-1", policy: { roles: {} }, revision: 1, evidence: [], actions: ["weiter", "pruefen"], progressAt: "2026-10-12T08:00:00Z", ...patch };
}
function gate(patch: Record<string, unknown> = {}) {
	return work({ state: "waiting_human", actions: ["go"], sha: "commit-a", branch: "feature/a", gate: { id: "gate-a", title: "Publish login", kind: "publication", exactApproval: "PUBLISH-GO feature/a" }, ...patch });
}
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => mocks.navigate }));
vi.mock("../stores/ui-store", () => ({ useUiStore: (select: (value: unknown) => unknown) => select({ openProjectSettings: mocks.projectSettings }) }));
vi.mock("./InstructorView", () => ({ InstructorNewDialog: () => null, InstructorWorkDetails: () => null, InstructorDeliveryReconcile: () => null }));
vi.mock("./InstructorPolicyDialog", () => ({ InstructorPolicyDialog: () => null }));
vi.mock("./InstructorModelDialog", () => ({ InstructorModelDialog: () => null }));
vi.mock("./InstructorExceptionDialog", () => ({ InstructorExceptionDialog: () => null }));
vi.mock("./InstructorPlanning", () => ({ InstructorPlanning: () => null }));
vi.mock("./chat/ReviewerChatSurface", () => ({ ReviewerChatSurface: () => null }));
vi.mock("../hooks/useInstructorMembers", () => ({ useInstructorMembers: (id: string | null) => { mocks.membersRead(id); return { data: id ? mocks.members : null, isLoading: false, error: null }; } }));
vi.mock("../hooks/useInstructor", () => ({
	instructorQueryKey: ["instructor"], instructorBoardQueryKey: ["instructor", "board"],
	useInstructorCapabilities: () => ({ data: { enabled: mocks.enabled, commands, publication: mocks.publication }, isLoading: false, error: null }),
	useInstructorBoardSnapshot: () => ({ data: mocks.snapshot, refetch: mocks.refetch, isFetching: false, isLoading: false, error: null }),
	useInstructorInventory: () => ({ data: { projects: [{ id: "project-a", name: "Project A" }, { id: "project-b", name: "Project B" }] }, refetch: mocks.inventoryRefetch, error: null }),
	useInstructorPolicy: () => ({ data: { roles: { instructor: { harness: "codex", model: "native-model", effort: "maximum", effortByModel: { "native-model": "native-high" } } } } }),
	useInstructorCommand: () => ({ mutateAsync: mocks.command, isPending: false, data: null }),
	useInstructorDetails: () => ({ data: mocks.details, isLoading: false, error: null }),
}));
function mount() {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
	const result = render(<QueryClientProvider client={client}><InstructorBoard /></QueryClientProvider>);
	return { ...result, redraw: () => result.rerender(<QueryClientProvider client={client}><InstructorBoard /></QueryClientProvider>) };
}
describe("Instructor board", () => {
	beforeEach(() => {
		vi.clearAllMocks(); mocks.enabled = true; mocks.publication = { available: true, reason: "" };
		mocks.snapshot = { id: "snapshot-a", createdAt: "2026-10-12T08:00:00Z", works: [work()], commands };
		mocks.command.mockResolvedValue({ dispatches: [] }); mocks.refetch.mockResolvedValue({ data: mocks.snapshot }); mocks.inventoryRefetch.mockResolvedValue({});
		mocks.members = null; mocks.details = null;
	});
	it("groups durable works by project without an always-mounted agent inventory", () => {
		mocks.snapshot = { ...mocks.snapshot, works: [work(), work({ id: "work-2", projectId: "project-b", name: "Release" })] };
		mount();
		expect(screen.getByRole("heading", { name: "Project A" })).toBeVisible();
		expect(screen.getByRole("heading", { name: "Project B" })).toBeVisible();
		expect(screen.getAllByRole("article")).toHaveLength(2);
		expect(mocks.membersRead).toHaveBeenCalledWith(null);
		expect(screen.queryByText("Runtime resource inventory")).not.toBeInTheDocument();
		expect(screen.getByText("codex · native-model · native-high")).toBeVisible();
	});
	it("opens the actual project agent configuration directly", async () => {
		mount();
		await userEvent.click(screen.getByRole("button", { name: "Project orchestrator · Model settings" }));
		expect(mocks.projectSettings).toHaveBeenCalledExactlyOnceWith("project-a", "agents");
	});
	it("only runs daemon-admitted actions and refreshes live state after a command", async () => {
		mount();
		await userEvent.click(screen.getByRole("button", { name: "Continue" }));
		expect(mocks.command).toHaveBeenCalledExactlyOnceWith({ command: "weiter", workIds: ["work-1"], snapshotId: "snapshot-a", idempotencyKey: expect.any(String) });
		expect(mocks.refetch).toHaveBeenCalledOnce();
		expect(screen.queryByRole("button", { name: "Sign and continue" })).not.toBeInTheDocument();
	});
	it("captures gate identity while the overview keeps updating", async () => {
		mocks.snapshot = { ...mocks.snapshot, works: [gate()] };
		const app = mount();
		await userEvent.click(screen.getByRole("button", { name: "Review gate" }));
		const dialog = screen.getByRole("dialog", { name: "Review approvals" });
		expect(within(dialog).getByRole("button", { name: "Sign and continue" })).toBeDisabled();
		mocks.snapshot = { ...mocks.snapshot, id: "snapshot-new", works: [gate({ sha: "commit-new", revision: 2, branch: "feature/new", gate: { title: "Changed gate", kind: "publication", exactApproval: "PUBLISH-GO feature/new" } })] };
		app.redraw();
		expect(within(dialog).getByText("commit-a", { exact: true })).toBeVisible();
		expect(within(dialog).queryByText("commit-new", { exact: true })).not.toBeInTheDocument();
		await userEvent.click(within(dialog).getByRole("checkbox", { name: "Approve Login" }));
		await userEvent.click(within(dialog).getByRole("button", { name: "Sign and continue" }));
		expect(mocks.command).toHaveBeenCalledExactlyOnceWith({ command: "go", workIds: ["work-1"], snapshotId: "snapshot-a", approvals: [{ workId: "work-1", approval: "PUBLISH-GO feature/a" }], idempotencyKey: expect.any(String) });
	});
	it("keeps publication closed without an available isolation boundary but permits reversible continuation", async () => {
		mocks.publication = { available: false, reason: "Protected publisher unavailable" };
		mocks.snapshot = { ...mocks.snapshot, works: [gate(), work({ id: "ready", name: "Another work" })] };
		mount();
		await userEvent.click(screen.getByRole("button", { name: "Continue" }));
		await userEvent.click(screen.getByRole("button", { name: "Review gate" }));
		expect(screen.getByText("Protected publisher unavailable")).toBeVisible();
		expect(screen.getByRole("checkbox", { name: "Approve Login" })).toBeDisabled();
		expect(screen.getByRole("button", { name: "Sign and continue" })).toBeDisabled();
		expect(mocks.command.mock.calls.map((call) => call[0].command)).toEqual(["weiter"]);
	});
	it("shows conditions of a non-admitted gate without inventing Go", async () => {
		mocks.snapshot = { ...mocks.snapshot, works: [gate({ actions: [], gate: { kind: "checks", title: "Tests missing" } })] };
		mount(); await userEvent.click(screen.getByRole("button", { name: "Review gate" }));
		expect(screen.getByText("The daemon has not admitted approval of this gate. Resolve the displayed condition first.")).toBeVisible();
		expect(screen.queryByRole("checkbox", { name: "Approve Login" })).not.toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Sign and continue" })).toBeDisabled();
		expect(mocks.command).not.toHaveBeenCalled();
	});
	it("admits mixed branch signatures as one ordered batch and retains the same intent on retry", async () => {
		mocks.snapshot = { ...mocks.snapshot, works: [gate(), gate({ id: "work-2", name: "Release", gate: { kind: "publication", title: "Publish release", exactApproval: "PUBLISH-GO feature/b" } })] };
		mocks.command.mockRejectedValueOnce(new Error("Admission result unavailable"));
		mount();
		await userEvent.click(screen.getByRole("checkbox", { name: "Select Login" }));
		await userEvent.click(screen.getByRole("checkbox", { name: "Select Release" }));
		await userEvent.click(screen.getByRole("button", { name: "Review approvals" }));
		await userEvent.click(screen.getByRole("checkbox", { name: "Approve Login" }));
		await userEvent.click(screen.getByRole("checkbox", { name: "Approve Release" }));
		await userEvent.click(screen.getByRole("button", { name: "Sign and continue" }));
		expect(mocks.command).toHaveBeenCalledOnce();
		const request = mocks.command.mock.calls[0][0];
		expect(request).toMatchObject({ command: "go", snapshotId: "snapshot-a", workIds: ["work-1", "work-2"], approvals: [{ workId: "work-1", approval: "PUBLISH-GO feature/a" }, { workId: "work-2", approval: "PUBLISH-GO feature/b" }] });
		const dialog = screen.getByRole("dialog", { name: "Review approvals" });
		mocks.command.mockResolvedValueOnce({ batchId: "cohort-one", dispatches: [] });
		await userEvent.click(within(dialog).getByRole("button", { name: "Sign and continue" }));
		expect(mocks.command.mock.calls[1][0]).toEqual(request);
	});

	it("loads verified members on demand, preserves unknown confirmation and refuses a reused legacy identity", async () => {
		mocks.members = { capturedAt: "2026-10-12T08:00:00Z", workRevision: 1, coordinator: {}, members: [
			{ id: "binding-current", name: "Current worker", role: "worker", association: "current", status: "running", requested: { harness: "codex", model: "native-model", effort: "native-high" }, openTarget: { kind: "session", projectId: "project-a", sessionId: "session-a" } },
			{ id: "binding-legacy", name: "Old worker", role: "worker", association: "legacy_unknown", status: "unknown", reason: "Session ID reused" },
		] };
		mount(); expect(mocks.membersRead).toHaveBeenLastCalledWith(null);
		await userEvent.click(screen.getByRole("button", { name: "Agents for Login" }));
		expect(mocks.membersRead).toHaveBeenLastCalledWith("work-1");
		await waitFor(() => expect(screen.getAllByRole("button", { name: "Open agent" })).toHaveLength(1));
		expect(screen.getByText("Session ID reused")).toBeVisible();
		expect(screen.getByText(/Identity not verified/)).toBeVisible();
		expect(screen.getAllByText("Unknown", { exact: true }).length).toBeGreaterThan(1);
		await userEvent.click(screen.getByRole("button", { name: "Open agent" }));
		expect(mocks.navigate).toHaveBeenCalledExactlyOnceWith({ to: "/projects/$projectId/sessions/$sessionId", params: { projectId: "project-a", sessionId: "session-a" } });
	});

	it("retains a gate that the daemon skips instead of presenting it as successfully signed", async () => {
		mocks.snapshot = { ...mocks.snapshot, works: [gate()] };
		mocks.command.mockResolvedValue({ dispatches: [], skipped: ["work-1: action unavailable"] });
		mount();
		await userEvent.click(screen.getByRole("button", { name: "Review gate" }));
		await userEvent.click(screen.getByRole("checkbox", { name: "Approve Login" }));
		await userEvent.click(screen.getByRole("button", { name: "Sign and continue" }));
		const dialog = screen.getByRole("dialog", { name: "Review approvals" });
		expect(within(dialog).getByRole("heading", { name: "Login" })).toBeVisible();
		expect(within(dialog).getByRole("alert")).toHaveTextContent("work-1: action unavailable");
		expect(mocks.command).toHaveBeenCalledOnce();
	});

	it("shows blocked and external wait conditions without inventing a Human gate", async () => {
		const blocked = work({ state: "blocked", waitCondition: "Native result receipt missing", actions: ["pruefen"] });
		mocks.snapshot = { ...mocks.snapshot, works: [blocked, work({ id: "external", name: "Database window", state: "waiting_external", waitCondition: "Wait for the database maintenance window", waitUntil: "2026-10-12T18:00:00Z", actions: [] })] };
		mocks.details = { work: blocked, events: [], dispatches: [] };
		mount();
		expect(screen.getByText("Native result receipt missing")).toBeVisible();
		expect(screen.getByText("Wait for the database maintenance window")).toBeVisible();
		expect(screen.queryByRole("button", { name: "Review gate" })).not.toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: "Login" }));
		const dialog = screen.getByRole("dialog", { name: "Login" });
		expect(within(dialog).getByText("Native result receipt missing")).toBeVisible();
		expect(mocks.command).not.toHaveBeenCalled();
	});
});
