import { useCallback, useEffect, useRef, useState } from "react";
import { aoBridge } from "../lib/bridge";
import { connectHost, connectedHosts, disconnectHost } from "../lib/host-clients";
import { useUiStore } from "../stores/ui-store";

const HOSTS_CHANGED_EVENT = "ao:remote-hosts-changed";

export function requestRemoteHostsRefresh(): void {
	window.dispatchEvent(new Event(HOSTS_CHANGED_EVENT));
}

export type RemoteHost = {
	hostId: string;
	label: string;
	url: string;
	status: "connecting" | "connected" | "offline";
};

export function useRemoteHosts(): { hosts: RemoteHost[]; refresh: () => Promise<void> } {
	const enabled = useUiStore((state) => state.remoteHosts);
	const enabledRef = useRef(enabled);
	enabledRef.current = enabled;
	const [hosts, setHosts] = useState<RemoteHost[]>([]);
	const refreshGeneration = useRef(0);
	const refresh = useCallback(async () => {
		if (!enabledRef.current) return;
		const generation = ++refreshGeneration.current;
		const current = () => enabledRef.current && refreshGeneration.current === generation;
		const saved = await aoBridge.remotes.list();
		if (!current()) return;
		setHosts(saved.map((host) => ({ ...host, status: "connecting" })));
		await Promise.all(saved.map(async (savedHost) => {
			let status: RemoteHost["status"] = "connected";
			let connectedHostId = savedHost.hostId;
			try {
				connectedHostId = (await connectHost(savedHost.url)).hostId;
				if (!enabledRef.current) {
					await disconnectHost(connectedHostId);
					return;
				}
			} catch {
				status = "offline";
				if (!current()) return;
				// A failed reconnect must not leave the old proxy marked connected.
				try { await disconnectHost(savedHost.hostId); } catch { /* Keep the offline state visible. */ }
			}
			if (!current()) return;
			setHosts((current) => current.map((host) => host.url === savedHost.url ? { ...host, hostId: connectedHostId, status } : host));
		}));
	}, []);

	useEffect(() => {
		if (enabled) {
			void refresh();
			return;
		}
		setHosts([]);
		for (const hostId of connectedHosts()) void disconnectHost(hostId);
	}, [enabled, refresh]);
	useEffect(() => {
		if (!enabled) return;
		const onChanged = () => { void refresh(); };
		window.addEventListener(HOSTS_CHANGED_EVENT, onChanged);
		return () => window.removeEventListener(HOSTS_CHANGED_EVENT, onChanged);
	}, [enabled, refresh]);

	return { hosts, refresh };
}
