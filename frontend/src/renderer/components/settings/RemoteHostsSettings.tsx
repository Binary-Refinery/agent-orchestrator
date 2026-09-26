import { useEffect, useState, type FormEvent } from "react";
import { aoBridge } from "../../lib/bridge";
import { connectHost, disconnectHost } from "../../lib/host-clients";
import { requestRemoteHostsRefresh } from "../../hooks/useRemoteHosts";
import { useUiStore } from "../../stores/ui-store";

type SavedHost = { hostId: string; label: string; url: string };

export function RemoteHostsSettings() {
	const [saved, setSaved] = useState<SavedHost[]>([]);
	const [label, setLabel] = useState("");
	const [url, setUrl] = useState("");
	const [password, setPassword] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	useEffect(() => {
		void aoBridge.remotes.list().then(setSaved).catch(() => setError("Could not load saved hosts."));
	}, []);
	const add = async (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		if (!label.trim() || !url.trim() || !password) return;
		setBusy(true);
		setError(null);
		try {
			const health = await aoBridge.remotes.add({ label: label.trim(), url: url.trim(), password });
			if (health !== "online") throw new Error(`Host is ${health}. Check its address and password.`);
			if (!useUiStore.getState().remoteHosts) return;
			const connected = await connectHost(url.trim());
			if (!useUiStore.getState().remoteHosts) {
				await disconnectHost(connected.hostId);
				return;
			}
			setSaved(await aoBridge.remotes.list());
			setLabel(""); setUrl(""); setPassword("");
			requestRemoteHostsRefresh();
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : "Could not add host.");
		} finally {
			setBusy(false);
		}
	};
	const remove = async (host: SavedHost) => {
		setError(null);
		try {
			await aoBridge.remotes.remove(host.url);
			await disconnectHost(host.hostId);
			setSaved(await aoBridge.remotes.list());
			requestRemoteHostsRefresh();
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : "Could not remove host.");
		}
	};
	return <div className="space-y-3 px-1 pb-4" data-testid="remote-hosts-settings">
		<p className="text-xs text-muted-foreground">Connect to AO running on another machine. Use a trusted private network.</p>
		<form className="flex flex-wrap items-end gap-2" onSubmit={(event) => void add(event)}>
			<label className="flex min-w-28 flex-1 flex-col gap-1 text-xs">Name<input className="rounded border bg-background p-2 text-sm" aria-label="Name" value={label} onChange={(event) => setLabel(event.target.value)} required /></label>
			<label className="flex min-w-40 flex-2 flex-col gap-1 text-xs">Address<input className="rounded border bg-background p-2 text-sm" aria-label="Address" placeholder="http://machine:port" value={url} onChange={(event) => setUrl(event.target.value)} required /></label>
			<label className="flex min-w-28 flex-1 flex-col gap-1 text-xs">Connection password<input className="rounded border bg-background p-2 text-sm" aria-label="Connection password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} required /></label>
			<button type="submit" className="rounded border px-3 py-2 text-sm" disabled={busy}>Add host</button>
		</form>
		{error && <p role="alert" className="text-sm text-destructive">{error}</p>}
		{saved.map((host) => <div key={host.url} className="flex items-center justify-between gap-3 rounded border px-3 py-2 text-sm">
			<span className="min-w-0 truncate">{host.label} <span className="text-muted-foreground">{host.url}</span>
				{!host.hostId && <span className="block text-xs text-muted-foreground">Re-pair required: re-enter its address and password above.</span>}
			</span>
			<button type="button" className="shrink-0 text-destructive" onClick={() => void remove(host)}>Remove</button>
		</div>)}
	</div>;
}
