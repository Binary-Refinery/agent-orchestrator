import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ServerConfig } from "./config";

const plain = new Map<string, string>();
const secure = new Map<string, string>();

vi.mock("@react-native-async-storage/async-storage", () => ({
	default: {
		getItem: vi.fn(async (key: string) => plain.get(key) ?? null),
		setItem: vi.fn(async (key: string, value: string) => void plain.set(key, value)),
		removeItem: vi.fn(async (key: string) => void plain.delete(key)),
	},
}));
vi.mock("expo-secure-store", () => ({
	getItemAsync: vi.fn(async (key: string) => secure.get(key) ?? null),
	setItemAsync: vi.fn(async (key: string, value: string) => void secure.set(key, value)),
	deleteItemAsync: vi.fn(async (key: string) => void secure.delete(key)),
}));
vi.mock("expo-constants", () => ({ default: { expoConfig: { extra: { eas: { projectId: "test-project" } } } } }));
vi.mock("expo-device", () => ({ isDevice: true, deviceName: "phone" }));
vi.mock("expo-notifications", () => ({
	getPermissionsAsync: vi.fn(async () => ({ status: "granted", canAskAgain: true })),
	getExpoPushTokenAsync: vi.fn(async () => ({ data: "ExponentPushToken[test]" })),
}));
vi.mock("react-native", () => ({ Platform: { OS: "ios" }, Linking: { openSettings: vi.fn() } }));
vi.mock("./installId", () => ({ getInstallId: vi.fn(async () => "phone-install-id") }));
vi.mock("./api", () => ({
	ApiError: class ApiError extends Error {},
	registerPushDevice: vi.fn(async () => {}),
	unregisterPushDevice: vi.fn(async () => {}),
	unpairFromDaemon: vi.fn(async () => {}),
}));

const { getPushStatus, registerForPush, unregisterFromPush } = await import("./push");
const { unpairFromDaemon, unregisterPushDevice } = await import("./api");
const { forgetServer } = await import("./disconnect");
const { loadHosts, saveHost, setActiveHost } = await import("./hosts");

function config(hostId: string, host = "192.168.1.42"): ServerConfig {
	return { hostId, host, httpPort: "3011", muxPort: "", secure: false, password: `token-${hostId}` };
}

describe("push registration across machines", () => {
	beforeEach(() => {
		plain.clear();
		secure.clear();
		vi.clearAllMocks();
		vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => ({ hostId: "h_b" }) })));
	});

	it("does not send A's bearer to B when B takes A's old address", async () => {
		await registerForPush(config("h_a"));
		await registerForPush(config("h_b"));

		expect(unregisterPushDevice).not.toHaveBeenCalled();
		expect((await getPushStatus(config("h_b"))).registered).toBe(true);
	});

	it("unregisters A when its endpoint still proves A's identity", async () => {
		await registerForPush(config("h_a"));
		vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => ({ hostId: "h_a" }) })));

		await registerForPush(config("h_b", "100.101.102.103"));

		expect(unregisterPushDevice).toHaveBeenCalledWith(config("h_a"), "ExponentPushToken[test]");
	});

	it("keeps one registration when the same machine changes address", async () => {
		await registerForPush(config("h_a"));
		await registerForPush(config("h_a", "100.101.102.103"));

		expect(unregisterPushDevice).not.toHaveBeenCalled();
	});

	it("replaces a legacy registration without sending its bearer to an unverified address", async () => {
		await registerForPush({ ...config("h_a"), hostId: undefined });
		expect((await getPushStatus(config("h_a"))).registered).toBe(false);
		await registerForPush(config("h_a"));

		expect(unregisterPushDevice).not.toHaveBeenCalled();
		expect((await getPushStatus(config("h_a"))).registered).toBe(true);
	});

	it("uses the address fallback while both registrations lack host IDs", async () => {
		const legacy = { ...config("h_a"), hostId: undefined };
		await registerForPush(legacy);
		await registerForPush(legacy);

		expect(unregisterPushDevice).not.toHaveBeenCalled();
	});

	it("replays only pending unregisters whose old host identity is verified", async () => {
		const old = { token: "ExponentPushToken[old]", host: "192.168.1.42", httpPort: "3011", secure: false, password: "old-secret" };
		secure.set("ao.pushPendingUnregister", JSON.stringify([{ ...old, hostId: "h_a" }, old]));

		await registerForPush(config("h_b"));
		expect(unregisterPushDevice).not.toHaveBeenCalled();

		vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => ({ hostId: "h_a" }) })));
		await registerForPush(config("h_b"));
		expect(unregisterPushDevice).toHaveBeenCalledExactlyOnceWith(
			expect.objectContaining({ hostId: "h_a", host: old.host, password: old.password }),
			"ExponentPushToken[old]",
		);
		expect(secure.has("ao.pushPendingUnregister")).toBe(false);
	});

	it("does not show A's registration as enabled while B is selected", async () => {
		await registerForPush(config("h_a"));

		expect((await getPushStatus(config("h_b"))).registered).toBe(false);
		expect((await getPushStatus(config("h_a"))).registered).toBe(true);
	});

	it("does not unregister A when B's notification switch is turned off", async () => {
		await registerForPush(config("h_a"));

		await unregisterFromPush(config("h_b"));

		expect(unregisterPushDevice).not.toHaveBeenCalled();
		expect((await getPushStatus(config("h_a"))).registered).toBe(true);
	});

	it("turns A's local switch off without sending A's bearer to a replacement host", async () => {
		await registerForPush(config("h_a"));

		await unregisterFromPush(config("h_a"));

		expect(unregisterPushDevice).not.toHaveBeenCalled();
		expect((await getPushStatus(config("h_a"))).registered).toBe(false);
	});

	it("forgets a matching legacy registration locally without sending its old bearer", async () => {
		const legacy = { ...config("h_a"), hostId: undefined, password: "legacy-secret" };
		await registerForPush(legacy);
		await saveHost({
			id: "h_a", name: "A", platform: "linux",
			endpoints: [{ kind: "lan", host: legacy.host, port: 3011, secure: false }],
			token: "current-pairing-token", lastConnected: 1,
		});
		await setActiveHost("h_a");
		vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => ({ hostId: "h_a" }) })));

		expect((await getPushStatus(legacy)).registered).toBe(true);
		await forgetServer();

		expect((await getPushStatus(legacy)).registered).toBe(false);
		expect(unregisterPushDevice).not.toHaveBeenCalled();
		expect(unpairFromDaemon).toHaveBeenCalledExactlyOnceWith(
			expect.objectContaining({ hostId: "h_a", password: "current-pairing-token" }),
			"phone-install-id",
		);
	});

	it("forgets selected B without unpairing A or clearing A's push registration", async () => {
		await registerForPush(config("h_a"));
		await saveHost({ id: "h_a", name: "A", platform: "darwin", endpoints: [], token: "token-h_a", lastConnected: 1 });
		await saveHost({
			id: "h_b", name: "B", platform: "linux",
			endpoints: [{ kind: "lan", host: "192.168.1.42", port: 3011, secure: false }],
			token: "token-h_b", lastConnected: 2,
		});
		await setActiveHost("h_b");

		await forgetServer();

		expect(unpairFromDaemon).toHaveBeenCalledExactlyOnceWith(
			expect.objectContaining({ hostId: "h_b", host: "192.168.1.42", password: "token-h_b" }),
			"phone-install-id",
		);
		expect((await loadHosts()).map((host) => host.id)).toEqual(["h_a"]);
		expect((await getPushStatus(config("h_a"))).registered).toBe(true);
	});

	it("clears B's registration when forgetting B with no reachable endpoints", async () => {
		await registerForPush(config("h_b"));
		await saveHost({ id: "h_b", name: "B", platform: "linux", endpoints: [], token: "token-h_b", lastConnected: 2 });
		await setActiveHost("h_b");

		await forgetServer();

		expect((await getPushStatus(config("h_b"))).registered).toBe(false);
	});

	it("does not send B's credential when its saved address answers as A", async () => {
		await registerForPush(config("h_a"));
		await saveHost({
			id: "h_b", name: "B", platform: "linux",
			endpoints: [{ kind: "lan", host: "192.168.1.42", port: 3011, secure: false }],
			token: "token-h_b", lastConnected: 2,
		});
		await setActiveHost("h_b");
		vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => ({ hostId: "h_a" }) })));

		await forgetServer();

		expect(unpairFromDaemon).not.toHaveBeenCalled();
		expect((await getPushStatus(config("h_a"))).registered).toBe(true);
	});
});
