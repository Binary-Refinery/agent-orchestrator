import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useUiStore } from "../../stores/ui-store";

const saved = vi.hoisted(() => ({ entries: [] as Array<{ hostId: string; label: string; url: string }> }));
const remotes = vi.hoisted(() => ({
	list: vi.fn(async () => saved.entries),
	add: vi.fn(async ({ label, url }: { label: string; url: string }) => {
		saved.entries = [{ hostId: "box-a", label, url }];
		return "online" as const;
	}),
	connect: vi.fn(async (url: string) => ({ hostId: "box-a", label: "Box A", url, base: "http://127.0.0.1:4000" })),
	disconnect: vi.fn(async () => undefined),
	remove: vi.fn(async () => undefined),
}));
vi.mock("../../lib/bridge", () => ({ aoBridge: { remotes } }));

import { RemoteHostsSettings } from "./RemoteHostsSettings";

afterEach(() => {
	saved.entries = [];
	useUiStore.setState({ remoteHosts: false });
	remotes.connect.mockClear();
});

it("does not connect a newly paired host after Remote hosts is turned off", async () => {
	useUiStore.setState({ remoteHosts: true });
	let finishAdd: ((health: "online") => void) | undefined;
	remotes.add.mockImplementationOnce(() => new Promise((resolve) => { finishAdd = resolve; }));
	render(<RemoteHostsSettings />);
	fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Box A" } });
	fireEvent.change(screen.getByRole("textbox", { name: "Address" }), { target: { value: "http://box-a:3001" } });
	fireEvent.change(screen.getByLabelText("Connection password"), { target: { value: "secret123" } });
	fireEvent.click(screen.getByRole("button", { name: "Add host" }));
	expect(screen.getByRole("button", { name: "Add host" })).toBeDisabled();
	useUiStore.setState({ remoteHosts: false });
	finishAdd?.("online");
	await waitFor(() => expect(screen.getByRole("button", { name: "Add host" })).toBeEnabled());
	expect(remotes.connect).not.toHaveBeenCalled();
});

it("pairs a host and shows it in the saved-host list", async () => {
	useUiStore.setState({ remoteHosts: true });
	render(<RemoteHostsSettings />);
	fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Box A" } });
	fireEvent.change(screen.getByRole("textbox", { name: "Address" }), { target: { value: "http://box-a:3001" } });
	fireEvent.change(screen.getByLabelText("Connection password"), { target: { value: "secret123" } });
	fireEvent.click(screen.getByRole("button", { name: "Add host" }));
	await waitFor(() => expect(screen.getByText("Box A")).toBeVisible());
});

it("explains how to re-pair a saved host without an identity", async () => {
	saved.entries = [{ hostId: "", label: "Old Box", url: "http://old-box:3001" }];
	render(<RemoteHostsSettings />);
	await screen.findByText(/Old Box/);
	expect(screen.getByText(/re-enter its address and password/i)).toBeVisible();
});
