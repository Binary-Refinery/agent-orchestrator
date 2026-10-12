import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { InstructorIssueIntake } from "./InstructorIssueIntake";

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock("../lib/api-client", () => ({ apiClient: { GET: mocks.get, POST: mocks.post }, apiErrorMessage: (error: { message: string }) => error.message }));
const issue = { number: 12, title: "Fix login", body: "Reproduce a login failure", url: "https://github.com/example/repo/issues/12", state: "open", labels: ["bug"], assignees: ["owner"], bodyTruncated: false };
const response = { projectId: "project-one", repository: "example/repo", issues: [issue], capturedAt: "2026-10-12T10:00:00Z", truncated: true };
function mount(onImported = vi.fn().mockResolvedValue(undefined)) {
	return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><InstructorIssueIntake projectId="project-one" onClose={vi.fn()} onImported={onImported} /></QueryClientProvider>);
}
describe("Instructor GitHub intake", () => {
	beforeEach(() => { vi.clearAllMocks(); mocks.get.mockResolvedValue({ data: response }); mocks.post.mockResolvedValue({ data: { id: "actual-work", projectId: "project-one" } }); });
	it("previews one repository issue then imports without starting any worker", async () => {
		const imported = vi.fn().mockResolvedValue(undefined); mount(imported);
		await userEvent.selectOptions(await screen.findByLabelText("Issue reference"), "12");
		expect(screen.getByText("Reproduce a login failure")).toBeVisible();
		expect(mocks.post).not.toHaveBeenCalled();
		await userEvent.click(screen.getByRole("button", { name: "Import selected issue" }));
		await waitFor(() => expect(imported).toHaveBeenCalledWith({ id: "actual-work", projectId: "project-one" }));
		expect(mocks.post).toHaveBeenCalledExactlyOnceWith("/api/v1/instructor/projects/{projectId}/issues/intake", { params: { path: { projectId: "project-one" } }, body: { repository: "example/repo", number: 12 } });
	});
	it.each(["open", "in_progress", "review"])("imports normalized open state %s", async (state) => {
		mocks.get.mockResolvedValue({ data: { ...response, issues: [{ ...issue, state }] } });
		const imported = vi.fn().mockResolvedValue(undefined); mount(imported);
		await userEvent.selectOptions(await screen.findByLabelText("Issue reference"), "12");
		expect(screen.getByText("Open issue")).toBeVisible();
		expect(screen.queryByText("Closed issue")).not.toBeInTheDocument();
		expect(mocks.post).not.toHaveBeenCalled();
		await userEvent.click(screen.getByRole("button", { name: "Import selected issue" }));
		await waitFor(() => expect(imported).toHaveBeenCalledWith({ id: "actual-work", projectId: "project-one" }));
		expect(mocks.post).toHaveBeenCalledExactlyOnceWith("/api/v1/instructor/projects/{projectId}/issues/intake", { params: { path: { projectId: "project-one" } }, body: { repository: "example/repo", number: 12 } });
	});
	it.each(["closed", "done", "cancelled"])("keeps unlinked terminal state %s blocked", async (state) => {
		mocks.get.mockResolvedValue({ data: { ...response, issues: [{ ...issue, state }] } });
		mount();
		await userEvent.selectOptions(await screen.findByLabelText("Issue reference"), "12");
		expect(screen.getByText("Closed issue")).toBeVisible();
		expect(screen.getByRole("button", { name: "Import selected issue" })).toBeDisabled();
		expect(mocks.post).not.toHaveBeenCalled();
	});
	it("keeps a closed linked issue available for opening its existing work", async () => {
		mocks.get.mockResolvedValue({ data: { ...response, issues: [{ ...issue, state: "closed", linkedWorkId: "existing-work" }] } });
		mount();
		await userEvent.selectOptions(await screen.findByLabelText("Issue reference"), "12");
		expect(screen.getByRole("button", { name: "Open existing work" })).toBeEnabled();
	});
	it("looks up an older issue by exact number and refuses a new closed issue", async () => {
		mocks.get.mockImplementation((_path, options) => Promise.resolve({ data: options.params.query ? { ...response, truncated: false, issues: [{ ...issue, number: 200, title: "Old closed issue", state: "closed" }] } : response }));
		mount(); await userEvent.type(screen.getByLabelText("Issue number"), "200");
		await userEvent.click(screen.getByRole("button", { name: "Preview issue" }));
		await screen.findByRole("heading", { name: "#200 · Old closed issue" });
		expect(screen.getByText("Closed issue")).toBeVisible();
		expect(screen.getByRole("button", { name: "Import selected issue" })).toBeDisabled();
		expect(mocks.get).toHaveBeenLastCalledWith("/api/v1/instructor/projects/{projectId}/issues", expect.objectContaining({ params: { path: { projectId: "project-one" }, query: { number: 200 } } }));
		expect(mocks.post).not.toHaveBeenCalled();
	});
	it("opens an existing linked work and surfaces a failed lookup honestly", async () => {
		mocks.get.mockResolvedValueOnce({ data: { ...response, issues: [{ ...issue, linkedWorkId: "existing-work" }] } });
		mocks.post.mockResolvedValueOnce({ data: { id: "existing-work" } });
		const imported = vi.fn().mockResolvedValue(undefined); mount(imported);
		await userEvent.selectOptions(await screen.findByLabelText("Issue reference"), "12");
		await userEvent.click(screen.getByRole("button", { name: "Open existing work" }));
		await waitFor(() => expect(imported).toHaveBeenCalledWith({ id: "existing-work" }));
		mocks.get.mockResolvedValueOnce({ error: { message: "GitHub unavailable" } });
		await userEvent.type(screen.getByLabelText("Issue number"), "123");
		await userEvent.click(screen.getByRole("button", { name: "Preview issue" }));
		expect(await screen.findByRole("alert")).toHaveTextContent("GitHub unavailable");
	});
});
