import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AccountRemovalControl } from "./AccountRemovalControl";

const api = vi.hoisted(() => ({ GET: vi.fn(), POST: vi.fn(), DELETE: vi.fn() }));
vi.mock("../../lib/api-client", () => ({ apiClient: api }));
const impact = { accountId: "account-a", revision: 0, sessions: [
  { sessionId: "session-active", provider: "codex", bindingRevision: 7, stopped: false },
  { sessionId: "session-dormant", provider: "codex", bindingRevision: 3, stopped: true },
] };
const operation = { id: "remove-a", accountId: "account-a", phase: "requested", impact, canCancel: true, recoveryRequired: false, createdAt: "2026-09-28T00:00:00Z", updatedAt: "2026-09-28T00:00:00Z" };
const success = (data: unknown) => ({ data, response: new Response(null, { status: 200 }) });
function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={client}><AccountRemovalControl accountId="account-a" /></QueryClientProvider>);
}

describe("coordinated account removal controls", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    localStorage.clear();
    api.GET.mockImplementation(async path => success(path.endsWith("removal-impact") ? impact : operation));
  });

  it("requires impact confirmation and sends exact revision zero without optimistic deletion", async () => {
    show();
    expect(await screen.findByRole("region", { name: "Removal impact" })).toHaveTextContent("session-active");
    expect(screen.getByRole("region", { name: "Removal impact" })).toHaveTextContent("session-dormant");
    const submit = screen.getByRole("button", { name: "Request account removal" });
    expect(submit).toBeDisabled();
    fireEvent.click(screen.getByRole("checkbox", { name: /I confirm removal at revision 0/ }));
    let resolve!: (value: unknown) => void;
    api.POST.mockImplementation(() => new Promise(done => { resolve = done; }));
    fireEvent.click(submit);
    await waitFor(() => expect(api.POST).toHaveBeenCalledOnce());
    const body = api.POST.mock.calls[0][1].body;
    expect(api.POST.mock.calls[0][0]).toBe("/api/v1/accounts-manager/accounts/{accountId}/removals");
    expect(body).toEqual({ operationId: expect.any(String), expectedRevision: 0, confirmed: true });
    expect(screen.queryByText("Removal complete")).not.toBeInTheDocument();
    const accepted = { ...operation, id: body.operationId };
    api.GET.mockImplementation(async path => success(path.endsWith("removal-impact") ? impact : accepted));
    resolve(success(accepted));
    expect(await screen.findByRole("region", { name: "Removal operation" })).toHaveTextContent("Phase: requested");
    expect(screen.queryByText("Removal complete")).not.toBeInTheDocument();
    expect(api.DELETE).not.toHaveBeenCalled();
  });

  it("shows unavailable removal capability and never uses the older direct delete route", async () => {
    api.GET.mockResolvedValue({ error: { requestId: "remove-unavailable", message: "private-token" }, response: new Response(null, { status: 501 }) });
    show();
    expect(await screen.findByRole("alert")).toHaveTextContent("remove-unavailable");
    expect(screen.getByRole("alert")).not.toHaveTextContent("private-token");
    expect(screen.queryByRole("button", { name: "Request account removal" })).not.toBeInTheDocument();
    expect(api.POST).not.toHaveBeenCalled();
    expect(api.DELETE).not.toHaveBeenCalled();
  });

  it("rejects a stale impact revision and requires renewed confirmation", async () => {
    api.POST.mockResolvedValue({ error: { requestId: "removal-conflict" }, response: new Response(null, { status: 409 }) });
    show();
    fireEvent.click(await screen.findByRole("checkbox", { name: /I confirm removal at revision 0/ }));
    fireEvent.click(screen.getByRole("button", { name: "Request account removal" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("removal-conflict");
    expect(screen.getByRole("checkbox", { name: /I confirm removal at revision 0/ })).not.toBeChecked();
    expect(screen.getByRole("button", { name: "Request account removal" })).toBeDisabled();
    expect(screen.queryByText("Removal complete")).not.toBeInTheDocument();
  });

  it("recovers the same unresolved removal after remount and does not convert missing status into success", async () => {
    api.POST.mockRejectedValue(new Error("response lost"));
    const view = show();
    fireEvent.click(await screen.findByRole("checkbox", { name: /I confirm removal at revision 0/ }));
    fireEvent.click(screen.getByRole("button", { name: "Request account removal" }));
    await waitFor(() => expect(api.POST).toHaveBeenCalledOnce());
    const id = api.POST.mock.calls[0][1].body.operationId;
    view.unmount();
    api.GET.mockResolvedValue({ error: { requestId: "unknown-removal" }, response: new Response(null, { status: 404 }) });
    show();
    expect(await screen.findByText(`Unconfirmed removal ID: ${id}`)).toBeInTheDocument();
    expect(screen.queryByText("Removal complete")).not.toBeInTheDocument();
    expect(screen.queryByText("Phase: cancelled")).not.toBeInTheDocument();
    expect(api.POST).toHaveBeenCalledOnce();
    expect(api.DELETE).not.toHaveBeenCalled();
  });

  it.each([
    ["requested", true, "cancel", "Cancel account removal", "cancelled"],
    ["recovery_required", false, "retry", "Retry account removal", "complete"],
  ] as const)("recovers %s with an owned %s operation", async (phase, canCancel, action, label, nextPhase) => {
    localStorage.setItem("ao:account-removals:v1", JSON.stringify([{ accountId: "account-a", operationId: "remove-a" }]));
    let current = { ...operation, phase: phase as string, canCancel, recoveryRequired: phase === "recovery_required" };
    api.GET.mockImplementation(async () => success(current));
    let resolve!: (value: unknown) => void;
    api.POST.mockImplementation(() => new Promise(done => { resolve = done; }));
    show();
    const control = await screen.findByRole("button", { name: label });
    await waitFor(() => expect(control).toBeEnabled());
    if (!canCancel) expect(screen.queryByRole("button", { name: "Cancel account removal" })).not.toBeInTheDocument();
    fireEvent.click(control);
    await waitFor(() => expect(api.POST).toHaveBeenCalledOnce());
    expect(api.POST.mock.calls[0]).toEqual([`/api/v1/accounts-manager/removals/{operationId}/${action}`, { params: { path: { operationId: "remove-a" } } }]);
    expect(api.GET.mock.calls.filter(([path]) => path === "/api/v1/accounts-manager/removals/{operationId}").length).toBeGreaterThanOrEqual(2);
    expect(screen.getByRole("region", { name: "Removal operation" })).toHaveTextContent(`Phase: ${phase}`);
    current = { ...current, phase: nextPhase, canCancel: false, recoveryRequired: false };
    resolve(success(current));
    await waitFor(() => expect(screen.getByRole("region", { name: "Removal operation" })).toHaveTextContent(`Phase: ${nextPhase}`));
    expect(api.DELETE).not.toHaveBeenCalled();
  });

  it("fails closed on corrupt recovery references before any removal request", async () => {
    localStorage.setItem("ao:account-removals:v1", "{");
    show();
    expect(await screen.findByRole("alert")).toHaveTextContent("Saved removal recovery is unavailable");
    expect(api.POST).not.toHaveBeenCalled();
    expect(api.DELETE).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Request account removal" })).not.toBeInTheDocument();
  });
});
