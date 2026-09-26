import { afterEach, expect, it, vi } from "vitest";

const remotes = vi.hoisted(() => ({ connect: vi.fn(), disconnect: vi.fn(async () => undefined) }));
vi.mock("./bridge", () => ({ aoBridge: { remotes } }));

import { clientForHost, connectHost, connectedHosts, disconnectHost } from "./host-clients";

afterEach(async () => {
	for (const hostId of connectedHosts()) await disconnectHost(hostId);
	remotes.connect.mockReset();
});

it("never treats the local sentinel as a remote host", () => {
	expect(() => clientForHost("local")).toThrow(/not a remote host/);
});

it("re-pairing an address removes its old host identity from the active map", async () => {
	const url = "http://box:3001";
	remotes.connect
		.mockResolvedValueOnce({ hostId: "h_old", label: "Box", url, base: "http://127.0.0.1:4000/old" })
		.mockResolvedValueOnce({ hostId: "h_new", label: "Box", url, base: "http://127.0.0.1:4001/new" });
	await connectHost(url);
	await connectHost(url);
	expect(connectedHosts()).toEqual(["h_new"]);
});
