import { QueryClient, QueryClientProvider, focusManager } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useInstructorCommand, useInstructorSnapshot } from "./useInstructor";

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock("../lib/api-client", () => ({ apiClient: { GET: mocks.get, POST: mocks.post }, apiErrorMessage: (error: { error?: { message: string; requestId?: string } }) => `${error.error?.message} (${error.error?.requestId})` }));

function createWrapper() {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
	return function Wrapper({ children }: { children: ReactNode }) {
		return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
	};
}

describe("Instructor daemon boundary", () => {
	beforeEach(() => { vi.clearAllMocks(); focusManager.setFocused(undefined); });

	it("does not read work state when the capability is disabled", () => {
		renderHook(() => useInstructorSnapshot(false), { wrapper: createWrapper() });
		expect(mocks.get).not.toHaveBeenCalled();
	});

	it("holds the displayed snapshot until an explicit refresh", async () => {
		mocks.get.mockResolvedValue({ data: { id: "snapshot-1", createdAt: "2026-10-11T10:00:00Z", works: [], commands: [] } });
		const { result } = renderHook(() => useInstructorSnapshot(true), { wrapper: createWrapper() });
		await waitFor(() => expect(result.current.data?.id).toBe("snapshot-1"));
		act(() => { focusManager.setFocused(false); focusManager.setFocused(true); });
		expect(mocks.get).toHaveBeenCalledTimes(1);
		mocks.get.mockResolvedValue({ data: { id: "snapshot-2", createdAt: "2026-10-11T10:01:00Z", works: [], commands: [] } });
		await act(async () => { await result.current.refetch(); });
		await waitFor(() => expect(result.current.data?.id).toBe("snapshot-2"));
	});

	it("forwards the typed command unchanged and surfaces a conflict without retry", async () => {
		mocks.post.mockResolvedValue({ error: { error: { message: "Obtain a new snapshot", requestId: "req-123" } } });
		const { result } = renderHook(() => useInstructorCommand(), { wrapper: createWrapper() });
		const body = { command: "weiter", snapshotId: "snapshot-1", workIds: ["work-1"], idempotencyKey: "operator-intent-1" };
		await act(async () => { await expect(result.current.mutateAsync(body)).rejects.toThrow("Obtain a new snapshot (req-123)"); });
		expect(mocks.post).toHaveBeenCalledExactlyOnceWith("/api/v1/instructor/commands", { body });
	});
});
