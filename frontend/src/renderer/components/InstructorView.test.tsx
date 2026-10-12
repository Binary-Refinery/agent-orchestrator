import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { InstructorView } from "./InstructorView";

const mocks = vi.hoisted(() => ({
	enabled: true,
	snapshot: {} as Record<string, unknown>,
	command: vi.fn(),
	result: null as Record<string, unknown> | null,
	navigate: vi.fn(),
	snapshotEnabled: vi.fn(),
	details: null as Record<string, unknown> | null,
	resources: [] as Array<Record<string, unknown>>,
	reconcile: vi.fn(),
}));

const registry = [
	{ name: "Status", action: "status", description: "Full snapshot", mutates: false, snapshotRequired: false, aliases: [] },
	{ name: "Tore", action: "tore", description: "Human gates", mutates: false, snapshotRequired: false, aliases: [] },
	{ name: "Status Delta", action: "delta", description: "Snapshot delta", mutates: false, snapshotRequired: true, aliases: [] },
	{ name: "Weiter", action: "weiter", description: "Resume approved work", mutates: true, snapshotRequired: true, aliases: [] },
	{ name: "Go", action: "go", description: "Approve displayed gate", mutates: true, snapshotRequired: true, aliases: ["go all"] },
	{ name: "Details", action: "details", description: "Work details", mutates: false, snapshotRequired: false, aliases: [] },
	{ name: "Prüfen", action: "pruefen", description: "Request verification", mutates: true, snapshotRequired: true, aliases: [] },
	{ name: "Stopp", action: "stopp", description: "Pause planned work", mutates: true, snapshotRequired: true, aliases: [] },
	{ name: "Neu", action: "neu", description: "Register work", mutates: true, snapshotRequired: false, aliases: [] },
];

function work(patch: Record<string, unknown> = {}) {
	return { id: "work-1", projectId: "demo", name: "Login flow", goal: "Make login reliable", source: "issue", sourceRef: "#123", sessionIds: ["worker-1", "reviewer-1"], state: "ready", phase: "implementation", summary: "Requirements accepted", next: "Implement login", policyVersion: "rule-v1", policy: { roles: {} }, revision: 2, evidence: [], actions: ["details", "weiter", "stopp", "pruefen"], createdAt: "2026-10-11T10:00:00Z", updatedAt: "2026-10-11T10:02:00Z", progressAt: "2026-10-11T10:01:00Z", ...patch };
}

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => mocks.navigate }));
vi.mock("./InstructorPolicyDialog", () => ({ InstructorPolicyDialog: () => null }));
vi.mock("./InstructorExceptionDialog", () => ({ InstructorExceptionDialog: () => null }));
vi.mock("./InstructorPlanning", () => ({ InstructorPlanning: () => null }));
vi.mock("../hooks/useInstructor", () => ({
	useInstructorCapabilities: () => ({ data: { enabled: mocks.enabled, reason: "AO_INSTRUCTOR is disabled", commands: registry }, isLoading: false, error: null }),
	useInstructorSnapshot: (enabled: boolean) => { mocks.snapshotEnabled(enabled); return { data: mocks.snapshot, isLoading: false, error: null, refetch: vi.fn() }; },
	useInstructorInventory: () => ({ data: { capturedAt: "2026-10-11T10:03:00Z", atomic: false, projects: [{ id: "demo", name: "Demo" }], sessions: [{ id: "orphan-session", projectId: "demo", displayName: "Unassigned review", kind: "worker", harness: "codex", activityState: "idle", assignment: "unassigned", branch: "review/orphan" }], resources: mocks.resources, unassignedCount: 1 }, isLoading: false, error: null }),
	useInstructorDetails: () => ({ data: mocks.details, isLoading: false, error: null, refetch: vi.fn() }),
	useInstructorCommand: () => ({ mutateAsync: mocks.command, isPending: false, data: mocks.result }),
	useInstructorReconcile: () => ({ mutateAsync: mocks.reconcile, isPending: false }),
	useInstructorInventoryReconcile: () => ({ mutateAsync: vi.fn(), isPending: false, error: null }),
}));

describe("InstructorView", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mocks.enabled = true;
		mocks.result = null;
		mocks.details = null;
		mocks.resources = [];
		mocks.command.mockResolvedValue({ dispatches: [], skipped: [] });
		mocks.reconcile.mockResolvedValue(work());
		mocks.snapshot = { id: "snapshot-1", createdAt: "2026-10-11T10:03:00Z", works: [work()], commands: registry };
	});

	it("shows capability state without presenting disabled work as live", () => {
		mocks.enabled = false;
		render(<InstructorView />);
		expect(screen.getByText("Instructor is disabled")).toBeInTheDocument();
		expect(screen.getByText("AO_INSTRUCTOR is disabled")).toBeInTheDocument();
		expect(screen.queryByText("Login flow")).not.toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "New work" })).not.toBeInTheDocument();
		expect(mocks.snapshotEnabled).toHaveBeenCalledWith(false);
	});

	it("tracks one durable work while keeping unassociated sessions outside the work table", () => {
		render(<InstructorView />);
		expect(screen.getAllByText("Login flow")).toHaveLength(1);
		expect(screen.getByText("work-1")).toBeInTheDocument();
		expect(screen.getByRole("heading", { name: "Demo" })).toBeInTheDocument();
		expect(screen.getAllByRole("row")).toHaveLength(2);
		expect(screen.getByRole("heading", { name: "Runtime resource inventory" })).toBeInTheDocument();
		expect(screen.getByText(/Unassigned review/)).toBeInTheDocument();
	});

	it("displays daemon metrics from the frozen snapshot without deriving them from visible rows", () => {
		mocks.snapshot = { ...mocks.snapshot, metrics: { closedLast7Days: 7, gatesClearedLast24Hours: 13, stalled: 3, recordedViolations: 2, active: 8, gated: 4, parked: 6, verificationFailures: 5 } };
		render(<InstructorView />);
		expect(screen.getByLabelText("Portfolio metrics from this snapshot")).toBeInTheDocument();
		expect(screen.getByText("Closed · last 7 days").nextElementSibling).toHaveTextContent("7");
		expect(screen.getByText("Human gates cleared · 24 hours").nextElementSibling).toHaveTextContent("13");
		expect(screen.getByText("Blocked / stalled").nextElementSibling).toHaveTextContent("3");
		expect(screen.getByText("Recorded violations · 24 hours").nextElementSibling).toHaveTextContent("2");
	});

	it("shows unknown Git inventory and failed probes without treating them as clean resources", () => {
		mocks.resources = [{ id: "git-probe-1", kind: "repository", projectId: "demo", path: "/missing/repo", assignment: "unknown", status: "probe_failed", error: "Repository unavailable" }];
		render(<InstructorView />);
		expect(screen.getByText("Repository unavailable")).toHaveAttribute("role", "alert");
		expect(screen.getByText("probe_failed")).toBeInTheDocument();
		expect(screen.getByText(/repository · demo · unknown/)).toBeInTheDocument();
		expect(screen.queryByText("No unassociated Git resources or probe failures reported.")).not.toBeInTheDocument();
	});

	it("sends server-allowed commands with the displayed work IDs and snapshot", async () => {
		render(<InstructorView />);
		await userEvent.click(screen.getByRole("button", { name: "Weiter" }));
		expect(mocks.command).toHaveBeenCalledWith({ command: "weiter", snapshotId: "snapshot-1", workIds: ["work-1"], idempotencyKey: expect.any(String) });
		expect(screen.queryByRole("button", { name: "Go" })).not.toBeInTheDocument();
	});

	it("exposes the registered Human-gate snapshot command", async () => {
		render(<InstructorView />);
		await userEvent.click(screen.getByRole("button", { name: "Tore" }));
		expect(mocks.command).toHaveBeenCalledWith({ command: "tore", snapshotId: "snapshot-1", idempotencyKey: expect.any(String) });
	});

	it("shows the daemon's typed command contract on demand", async () => {
		mocks.snapshot = { ...mocks.snapshot, commands: registry.map((spec) => ({ ...spec, authority: "operator", scopes: ["work", "snapshot-cohort"], parameters: [{ name: "snapshotId", type: "snapshot-reference", required: true }], preconditions: ["current-work-revision", "action-eligible"] })) };
		render(<InstructorView />);
		await userEvent.click(screen.getByText("Command reference"));
		expect(screen.getAllByText("Authority: operator · Scope: work, snapshot-cohort")).toHaveLength(registry.length);
		expect(screen.getAllByText("snapshotId: snapshot-reference (required)")).toHaveLength(registry.length);
		expect(screen.getAllByText("Conditions: current-work-revision, action-eligible")).toHaveLength(registry.length);
	});

	it("requires operator-entered exact publication approval and preserves its captured snapshot", async () => {
		mocks.snapshot = { ...mocks.snapshot, works: [work({ state: "waiting_human", actions: ["details", "go"], gate: { id: "publish-gate", kind: "publication", title: "Publish feature", exactApproval: "PUBLISH-GO feature/login" } })] };
		render(<InstructorView />);
		await userEvent.click(screen.getByRole("button", { name: "Go" }));
		const dialog = screen.getByRole("dialog", { name: "Go" });
		const approval = within(dialog).getByRole("textbox", { name: "Enter the exact Human approval shown below" });
		expect(approval).toHaveValue("");
		expect(mocks.command).not.toHaveBeenCalled();
		await userEvent.type(approval, "PUBLISH-GO feature/login");
		await userEvent.click(within(dialog).getByRole("button", { name: "Go" }));
		expect(mocks.command).toHaveBeenCalledWith(expect.objectContaining({ command: "go", snapshotId: "snapshot-1", workIds: ["work-1"], approval: "PUBLISH-GO feature/login" }));
	});

	it("keeps ordinary multi-work Go in one explicit ordered cohort with a stable retry intent", async () => {
		mocks.snapshot = { ...mocks.snapshot, works: [
			work({ state: "waiting_human", actions: ["details", "go"], gate: { id: "scope-1", kind: "scope", title: "Approve scope" } }),
			work({ id: "work-2", name: "Search flow", state: "waiting_human", actions: ["details", "go"], gate: { id: "scope-2", kind: "scope", title: "Approve search" } }),
		] };
		mocks.command.mockRejectedValueOnce(new Error("Response lost"));
		render(<InstructorView />);
		await userEvent.click(screen.getByRole("checkbox", { name: "Select Login flow" }));
		await userEvent.click(screen.getByRole("checkbox", { name: "Select Search flow" }));
		await userEvent.selectOptions(screen.getByRole("combobox", { name: "Command" }), "go");
		await userEvent.click(screen.getByRole("button", { name: "Execute" }));
		expect(mocks.command).toHaveBeenCalledOnce();
		expect(mocks.command).toHaveBeenCalledWith(expect.objectContaining({ command: "go", snapshotId: "snapshot-1", workIds: ["work-1", "work-2"], approvals: [{ workId: "work-1", approval: "" }, { workId: "work-2", approval: "" }] }));
		expect(screen.queryByRole("dialog", { name: "Go" })).not.toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: "Execute" }));
		expect(mocks.command.mock.calls[1][0]).toEqual(mocks.command.mock.calls[0][0]);
	});

	it("sends matching typed publication groups with stable retry keys and never signs another branch", async () => {
		const publication = (id: string, name: string, branch: string) => work({ id, name, state: "waiting_human", actions: ["details", "go"], gate: { id: `gate-${id}`, kind: "publication", title: "Publish feature", exactApproval: `PUBLISH-GO ${branch}` } });
		mocks.snapshot = { ...mocks.snapshot, works: [publication("work-1", "Login flow", "feature/login"), publication("work-2", "Login tests", "feature/login"), publication("work-3", "Search flow", "feature/search")] };
		mocks.command.mockRejectedValueOnce(new Error("Response lost; retry the same admission"));
		const view = render(<InstructorView />);
		for (const name of ["Login flow", "Login tests", "Search flow"]) await userEvent.click(screen.getByRole("checkbox", { name: `Select ${name}` }));
		await userEvent.selectOptions(screen.getByRole("combobox", { name: "Command" }), "go");
		await userEvent.click(screen.getByRole("button", { name: "Execute" }));
		const dialog = screen.getByRole("dialog", { name: "Go" });
		const approval = within(dialog).getByRole("textbox", { name: "Enter the exact Human approval shown below" });
		await userEvent.type(approval, "PUBLISH-GO feature/wrong");
		expect(within(dialog).getByRole("button", { name: "Go" })).toBeDisabled();
		expect(mocks.command).not.toHaveBeenCalled();
		await userEvent.clear(approval); await userEvent.type(approval, "PUBLISH-GO feature/login");
		mocks.snapshot = { ...mocks.snapshot, id: "snapshot-live", works: [publication("work-1", "Login flow", "feature/changed")] };
		view.rerender(<InstructorView />);
		await userEvent.click(within(dialog).getByRole("button", { name: "Go" }));
		expect(mocks.command.mock.calls[0][0]).toMatchObject({ command: "go", snapshotId: "snapshot-1", workIds: ["work-1", "work-2"], approvals: [{ workId: "work-1", approval: "PUBLISH-GO feature/login" }, { workId: "work-2", approval: "PUBLISH-GO feature/login" }] });
		expect(mocks.command.mock.calls[0][0]).not.toHaveProperty("approval");
		await userEvent.click(within(dialog).getByRole("button", { name: "Go" }));
		expect(mocks.command.mock.calls[1][0]).toEqual(mocks.command.mock.calls[0][0]);
		expect(within(dialog).getByText("PUBLISH-GO feature/search")).toBeInTheDocument();
		expect(approval).toHaveValue("");
		await userEvent.type(approval, "PUBLISH-GO feature/search");
		await userEvent.click(within(dialog).getByRole("button", { name: "Go" }));
		expect(mocks.command.mock.calls[2][0]).toMatchObject({ snapshotId: "snapshot-1", workIds: ["work-3"], approval: "PUBLISH-GO feature/search" });
		expect(mocks.command.mock.calls[2][0].idempotencyKey).not.toBe(mocks.command.mock.calls[0][0].idempotencyKey);
		expect(screen.queryByRole("dialog", { name: "Go" })).not.toBeInTheDocument();
	});

	it("uses a registered all-command with the frozen cohort and leaves eligibility to the daemon", async () => {
		render(<InstructorView />);
		await userEvent.click(screen.getByRole("button", { name: "Go all" }));
		expect(mocks.command).toHaveBeenCalledWith({ command: "go all", snapshotId: "snapshot-1", idempotencyKey: expect.any(String) });
	});

	it("executes ordinary Human Go once without adding another approval round", async () => {
		mocks.snapshot = { ...mocks.snapshot, works: [work({ state: "waiting_human", actions: ["details", "go"], gate: { id: "scope-gate", kind: "scope", title: "Approve scope" } })] };
		render(<InstructorView />);
		await userEvent.click(screen.getByRole("button", { name: "Go" }));
		expect(mocks.command).toHaveBeenCalledTimes(1);
		expect(mocks.command).toHaveBeenCalledWith(expect.objectContaining({ command: "go", snapshotId: "snapshot-1", workIds: ["work-1"] }));
		expect(screen.queryByRole("dialog", { name: "Go" })).not.toBeInTheDocument();
	});

	it("shows deltas separately without replacing the snapshot used by later commands", async () => {
		mocks.command.mockResolvedValueOnce({ snapshot: { id: "delta-1", baseId: "snapshot-1", createdAt: "2026-10-11T10:04:00Z", works: [work({ summary: "Implementation complete" })], commands: registry }, dispatches: [], skipped: [] });
		render(<InstructorView />);
		await userEvent.click(screen.getByRole("button", { name: "Status Delta" }));
		expect(screen.getByText("Requirements accepted")).toBeInTheDocument();
		expect(screen.getByText(/Implementation complete/)).toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: "Weiter" }));
		expect(mocks.command.mock.calls[1][0].snapshotId).toBe("snapshot-1");
	});

	it("labels delivery separately from completion", () => {
		mocks.result = { dispatches: [{ id: "dispatch-1", workId: "work-1", action: "weiter", state: "delivered" }], skipped: [] };
		render(<InstructorView />);
		expect(screen.getByText("delivered")).toBeInTheDocument();
		expect(screen.getByText("Requirements accepted")).toBeInTheDocument();
		expect(screen.getByText(/Queued, delivered, accepted and progress are separate states/)).toBeInTheDocument();
	});

	it("registers intake using daemon policy rather than sending frontend governance defaults", async () => {
		render(<InstructorView />);
		await userEvent.click(screen.getByRole("button", { name: "New work" }));
		const dialog = screen.getByRole("dialog", { name: "New work" });
		await userEvent.selectOptions(within(dialog).getByRole("combobox", { name: "Project" }), "demo");
		await userEvent.type(within(dialog).getByRole("textbox", { name: "Short work name" }), "Search flow");
		await userEvent.type(within(dialog).getByRole("textbox", { name: "Goal" }), "Make search reliable");
		await userEvent.type(within(dialog).getByRole("textbox", { name: "Source / issue reference" }), "#124");
		await userEvent.click(within(dialog).getByRole("button", { name: "New work" }));
		expect(mocks.command).toHaveBeenCalledWith(expect.objectContaining({ command: "neu", new: { projectId: "demo", name: "Search flow", goal: "Make search reliable", source: "operator", sourceRef: "#124" } }));
		expect(mocks.command.mock.calls[0][0].new).not.toHaveProperty("policy");
	});

	it("distinguishes requested and sent native configuration from missing provider confirmation", async () => {
		mocks.details = { work: work({ launches: [{ role: "worker", sessionId: "worker-1", status: "spawned", requested: { harness: "opencode", model: "provider/model", effort: "native-ultra" }, sent: { harness: "opencode", model: "provider/model", effort: "native-high" } }] }), events: [], dispatches: [] };
		render(<InstructorView />);
		await userEvent.click(screen.getByRole("button", { name: "Details" }));
		const dialog = screen.getByRole("dialog", { name: "Login flow" });
		expect(within(dialog).getByRole("heading", { name: "Agent launches" })).toBeInTheDocument();
		expect(within(dialog).getByText("opencode · provider/model · native-ultra")).toBeInTheDocument();
		expect(within(dialog).getByText("opencode · provider/model · native-high")).toBeInTheDocument();
		expect(within(dialog).getByText("Unknown")).toBeInTheDocument();
	});

	it("shows bounded verification and revoked evidence separately from verified claims", async () => {
		mocks.details = { work: work({ verification: { status: "waiting", passes: 2, maxPasses: 2, claims: 3, verified: 1, lastRunAt: "2026-10-11T10:04:00Z", nextRunAt: "0001-01-01T00:00:00Z" }, evidence: [{ id: "claim-1", kind: "review", role: "reviewer", phase: "review", phaseSequence: 2, ref: "review/run-1", sha: "abcd", verified: true, revoked: true, revocationReason: "Native review observation contradicted the claim", outcome: "passed" }] }), events: [], dispatches: [] };
		render(<InstructorView />);
		await userEvent.click(screen.getByRole("button", { name: "Details" }));
		const dialog = screen.getByRole("dialog", { name: "Login flow" });
		expect(within(dialog).getByText("Verification: waiting · passes 2 / 2")).toBeInTheDocument();
		expect(within(dialog).getByText("Claims: 3 · independently verified: 1")).toBeInTheDocument();
		expect(within(dialog).getByText(/review · reviewer · .*Revoked/)).toBeInTheDocument();
		expect(within(dialog).getByText("Native review observation contradicted the claim")).toBeInTheDocument();
	});

	it("keeps missing native effort confirmation unknown when only model confirmation exists", async () => {
		mocks.details = { work: work({ launches: [{ role: "worker", sessionId: "worker-1", status: "spawned", requested: { harness: "opencode", model: "provider/model", effort: "native-ultra" }, confirmed: { harness: "opencode", model: "provider/model", effort: "" } }] }), events: [], dispatches: [] };
		render(<InstructorView />);
		await userEvent.click(screen.getByRole("button", { name: "Details" }));
		expect(screen.getByText("opencode · provider/model · Unknown")).toBeInTheDocument();
	});

	it("requires an explicit observation for the daemon-bound uncertain dispatch without approving the suspended gate", async () => {
		mocks.details = { work: work({ gate: { id: "delivery-gate", kind: "delivery_uncertain", dispatchId: "dispatch-exact", targetSessionId: "orch-exact", title: "Delivery uncertain" }, suspendedGate: { id: "publication-gate", kind: "publication", title: "Publish feature", exactApproval: "PUBLISH-GO feature/login" } }), events: [], dispatches: [] };
		render(<InstructorView />);
		await userEvent.click(screen.getByRole("button", { name: "Details" }));
		const dialog = screen.getByRole("dialog", { name: "Login flow" });
		expect(within(dialog).getByText("Original Human gate remains pending: Publish feature")).toBeInTheDocument();
		expect(within(dialog).getByRole("button", { name: "Record observation" })).toBeDisabled();
		await userEvent.selectOptions(within(dialog).getByRole("combobox", { name: "Observed delivery" }), "absent");
		await userEvent.type(within(dialog).getByRole("textbox", { name: "Observation note" }), "Inspected the exact target conversation; this dispatch is absent.");
		await userEvent.click(within(dialog).getByRole("button", { name: "Record observation" }));
		expect(mocks.reconcile).toHaveBeenCalledWith({ snapshotId: "snapshot-1", workId: "work-1", dispatchId: "dispatch-exact", accepted: false, note: "Inspected the exact target conversation; this dispatch is absent." });
		expect(mocks.command).not.toHaveBeenCalled();
	});

	it.each(["dispatch", "snapshot"])("requires a new observation when the bound %s changes", async (changed) => {
		const uncertain = (dispatchId: string) => ({ work: work({ gate: { id: `gate-${dispatchId}`, kind: "delivery_uncertain", dispatchId, targetSessionId: "orch-exact", title: "Delivery uncertain" } }), events: [], dispatches: [] });
		mocks.details = uncertain("dispatch-original");
		const view = render(<InstructorView initialWorkId="work-1" />);
		await userEvent.selectOptions(screen.getByRole("combobox", { name: "Observed delivery" }), "absent");
		await userEvent.type(screen.getByRole("textbox", { name: "Observation note" }), "Inspected the original delivery.");
		expect(screen.getByRole("button", { name: "Record observation" })).toBeEnabled();
		if (changed === "dispatch") mocks.details = uncertain("dispatch-new");
		else mocks.snapshot = { ...mocks.snapshot, id: "snapshot-new" };
		view.rerender(<InstructorView initialWorkId="work-1" />);
		expect(screen.getByRole("combobox", { name: "Observed delivery" })).toHaveValue("");
		expect(screen.getByRole("textbox", { name: "Observation note" })).toHaveValue("");
		expect(screen.getByRole("button", { name: "Record observation" })).toBeDisabled();
		expect(mocks.reconcile).not.toHaveBeenCalled();
	});
});
