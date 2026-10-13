import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { InstructorWork } from "../hooks/useInstructor";
import { InstructorRecoveryDialog } from "./InstructorRecoveryDialog";

const mocks = vi.hoisted(() => ({ preview: vi.fn(), recover: vi.fn(), close: vi.fn(), recovered: vi.fn(), settings: vi.fn() }));
vi.mock("../hooks/useInstructorRecovery", () => ({ useInstructorRecoveryPreview: () => ({ mutateAsync: mocks.preview, isPending: false }), useRecoverInstructorSession: () => ({ mutateAsync: mocks.recover, isPending: false }) }));
vi.mock("../stores/ui-store", () => ({ useUiStore: (select: (value: unknown) => unknown) => select({ openProjectSettings: mocks.settings }) }));
const work = { id: "work-1", name: "Restore login", projectId: "demo", revision: 1 } as InstructorWork;
const captured = { workId: "work-1", workRevision: 1, policyVersion: "policy-old", sessionId: "demo-1", sessionCreatedAt: "2026-10-13T09:00:00Z", providerConversationId: "native-thread-1", controllerGeneration: "generation-4", ready: false, action: "resume", reasonCode: "CONTROLLER_MISSING", reason: "The existing conversation needs a controller." };
function view() { return render(<InstructorRecoveryDialog work={work} onClose={mocks.close} onRecovered={mocks.recovered} />); }

describe("InstructorRecoveryDialog", () => {
	beforeEach(() => { vi.clearAllMocks(); mocks.preview.mockResolvedValue(captured); mocks.recover.mockResolvedValue({ ...captured, ready: true, action: "none" }); mocks.recovered.mockResolvedValue(undefined); });
	it("requires an explicit recovery action and preserves the exact observed session incarnation", async () => {
		view(); await screen.findByText(captured.reason); expect(mocks.recover).not.toHaveBeenCalled();
		await userEvent.click(screen.getByRole("button", { name: "Reconnect existing conversation" }));
		expect(mocks.recover).toHaveBeenCalledExactlyOnceWith({ workId: "work-1", workRevision: 1, policyVersion: "policy-old", sessionId: "demo-1", sessionCreatedAt: captured.sessionCreatedAt, providerConversationId: "native-thread-1", controllerGeneration: "generation-4" });
		await screen.findByRole("status"); expect(mocks.recovered).toHaveBeenCalledOnce();
		expect(screen.queryByRole("button", { name: "Reconnect existing conversation" })).not.toBeInTheDocument();
	});
	it("shows a policy conflict and does not turn an unavailable action into a retry loop", async () => {
		mocks.preview.mockResolvedValue({ ...captured, action: "none", reasonCode: "PROJECT_POLICY_CONFLICT", reason: "Policy requires opencode; this session uses codex. Review the project policy." });
		view(); await screen.findByText(/Policy requires opencode/);
		expect(screen.queryByRole("button", { name: "Reconnect existing conversation" })).not.toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: "Open project settings" }));
		expect(mocks.settings).toHaveBeenCalledWith("demo", "agents"); expect(mocks.recover).not.toHaveBeenCalled();
	});
	it.each(["INSTRUCTOR_RESUME_BLOCKED", "UNSUPPORTED_EFFORT", "UNSUPPORTED_MODEL"])("offers corrective settings for real %s status even when resume is listed", async (code) => {
		mocks.preview.mockResolvedValue({ ...captured, reasonCode: code, reason: "The saved role and native selection are incompatible.", action: "resume" });
		view(); await screen.findByText("The saved role and native selection are incompatible.");
		expect(screen.getByRole("button", { name: "Reconnect existing conversation" })).toBeEnabled();
		await userEvent.click(screen.getByRole("button", { name: "Open project settings" }));
		expect(mocks.settings).toHaveBeenCalledWith("demo", "agents"); expect(mocks.recover).not.toHaveBeenCalled();
	});
	it.each(["INSTRUCTOR_RESUME_BLOCKED", "UNSUPPORTED_EFFORT", "UNSUPPORTED_MODEL"])("offers corrective settings after a generic preview receives POST %s", async (code) => {
		mocks.preview.mockResolvedValue({ ...captured, reasonCode: "CONTROLLER_UNAVAILABLE" });
		mocks.recover.mockRejectedValue(Object.assign(new Error("Role requires opencode, got codex. " + code + " · request-recovery-17"), { code, requestId: "request-recovery-17" }));
		view(); await screen.findByText(captured.reason);
		expect(screen.queryByRole("button", { name: "Open project settings" })).not.toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: "Reconnect existing conversation" }));
		expect(await screen.findByRole("alert")).toHaveTextContent("Role requires opencode, got codex.");
		expect(screen.getByRole("alert")).toHaveTextContent("request-recovery-17");
		expect(screen.getByRole("button", { name: "Reconnect existing conversation" })).toBeDisabled();
		await userEvent.click(screen.getByRole("button", { name: "Open project settings" }));
		expect(mocks.settings).toHaveBeenCalledWith("demo", "agents"); expect(mocks.recover).toHaveBeenCalledTimes(1);
	});

	it("allows an explicit retry after rechecking even when GET retains the previous failure", async () => {
		mocks.recover.mockRejectedValueOnce(Object.assign(new Error("Role requires opencode, got codex."), { code: "INSTRUCTOR_RESUME_BLOCKED" }));
		view(); await screen.findByText(captured.reason);
		await userEvent.click(screen.getByRole("button", { name: "Reconnect existing conversation" }));
		expect(await screen.findByRole("alert")).toHaveTextContent("Role requires opencode");
		expect(screen.getByRole("button", { name: "Reconnect existing conversation" })).toBeDisabled();
		// Saving settings leaves the old session probe reason cached. Only the
		// next explicit resume revalidates the corrected effective selection.
		mocks.preview.mockResolvedValue({ ...captured, reasonCode: "INSTRUCTOR_RESUME_BLOCKED", reason: "Role requires opencode, got codex." });
		await userEvent.click(screen.getByRole("button", { name: "Check again" }));
		await waitFor(() => expect(screen.getByRole("button", { name: "Reconnect existing conversation" })).toBeEnabled());
		expect(screen.getByRole("button", { name: "Open project settings" })).toBeEnabled();
		expect(mocks.recover).toHaveBeenCalledTimes(1);
		await userEvent.click(screen.getByRole("button", { name: "Reconnect existing conversation" }));
		await screen.findByRole("status"); expect(mocks.recover).toHaveBeenCalledTimes(2);
	});

	it("keeps the captured identity after a failed attempt until explicitly rechecked", async () => {
		mocks.recover.mockRejectedValueOnce(new Error("Session changed. Check again. Request: recovery-42"));
		view(); await screen.findByText(captured.reason);
		await userEvent.click(screen.getByRole("button", { name: "Reconnect existing conversation" }));
		await screen.findByRole("alert"); expect(mocks.preview).toHaveBeenCalledTimes(1);
		mocks.preview.mockResolvedValue({ ...captured, controllerGeneration: "generation-5", workRevision: 2 });
		await userEvent.click(screen.getByRole("button", { name: "Check again" }));
		await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(2));
		await userEvent.click(screen.getByRole("button", { name: "Reconnect existing conversation" }));
		expect(mocks.recover.mock.calls[1][0]).toMatchObject({ controllerGeneration: "generation-5", workRevision: 2 });
	});
});
