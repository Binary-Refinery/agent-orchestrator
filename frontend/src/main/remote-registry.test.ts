import { describe, expect, it } from "vitest";
import { RemoteRegistry } from "./remote-registry";

const workbox = { label: "workbox", url: "http://192.0.2.1:3011", password: "secret", hostId: "h_workbox" };

describe("connected remote hosts", () => {
	it("keeps two different machines connected at once", async () => {
		const registry = new RemoteRegistry(async (entry) => ({
			base: `http://127.0.0.1:7654/${entry.hostId}`,
			url: entry.url,
			close: async () => undefined,
		}));
		await registry.connect(workbox);
		await registry.connect({ hostId: "h_mini", label: "mini", url: "http://192.0.2.9:3011", password: "other" });
		expect(registry.views().map(({ hostId }) => hostId)).toEqual(["h_workbox", "h_mini"]);
	});

	it("closes an in-flight connection before app shutdown finishes", async () => {
		let finishStart!: (value: { base: string; url: string; close: () => Promise<void> }) => void;
		let closeCount = 0;
		const registry = new RemoteRegistry(async () => new Promise((resolve) => { finishStart = resolve; }));
		const connecting = registry.connect(workbox);
		const shutdown = registry.closeAll();
		await Promise.resolve();
		finishStart({
			base: "http://127.0.0.1:7654/token",
			url: workbox.url,
			close: async () => { closeCount++; },
		});
		await Promise.all([connecting, shutdown]);
		expect(closeCount).toBe(1);
		expect(registry.views()).toEqual([]);
		await expect(registry.connect(workbox)).rejects.toThrow(/closing/);
	});

	it("replaces a LAN proxy when the same host connects by Tailscale", async () => {
		const closed: string[] = [];
		const registry = new RemoteRegistry(async (entry) => ({
			base: `http://127.0.0.1:7654/${entry.url.includes("https") ? "tailscale" : "lan"}`,
			url: entry.url,
			close: async () => { closed.push(entry.url); },
		}));
		await registry.connect(workbox);
		await registry.connect({ ...workbox, url: "https://workbox.tailnet" });
		expect(registry.views()).toEqual([{
			hostId: "h_workbox",
			label: "workbox",
			url: "https://workbox.tailnet",
			base: "http://127.0.0.1:7654/tailscale",
		}]);
		expect(closed).toEqual([workbox.url]);
	});
	it("does not leave a proxy serving after a connect and disconnect overlap", async () => {
		let finishStart!: (value: { base: string; url: string; close: () => Promise<void> }) => void;
		let startCount = 0;
		let closeCount = 0;
		const registry = new RemoteRegistry(async () => {
			startCount++;
			return new Promise((resolve) => {
				finishStart = resolve;
			});
		});
		const first = registry.connect(workbox);
		const second = registry.connect(workbox);
		const disconnect = registry.disconnect(workbox.url);
		await Promise.resolve();
		finishStart({
			base: "http://127.0.0.1:7654/token",
			url: workbox.url,
			close: async () => { closeCount++; },
		});
		await Promise.all([first, second, disconnect]);
		expect(startCount).toBe(1);
		expect(closeCount).toBe(1);
		expect(registry.views()).toEqual([]);
	});
});
