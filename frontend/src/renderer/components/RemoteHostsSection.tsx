import type { RemoteHost } from "../hooks/useRemoteHosts";
import type { WorkspaceSummary } from "../types/workspace";

export function RemoteHostsSection({ hosts, workspaces, onOpenSession, onStart, onRetry }: {
	hosts: RemoteHost[];
	workspaces: WorkspaceSummary[];
	onOpenSession: (hostId: string, projectId: string, sessionId: string) => void;
	onStart: (hostId: string) => void;
	onRetry: () => void;
}) {
	if (hosts.length === 0) return null;
	return <section aria-label="Remote hosts" className="border-t px-3 py-3" data-testid="remote-hosts-section">
		<h2 className="mb-2 text-xs font-semibold text-muted-foreground">Remote hosts</h2>
		{hosts.map((host) => <div key={host.url} className="mb-3" data-host-id={host.hostId}>
			<div className="flex items-center justify-between gap-2 text-sm">
				<span className="min-w-0 truncate font-medium">{host.label}</span>
				{host.status === "offline" ? <button type="button" className="text-xs text-muted-foreground underline" onClick={onRetry}>Retry {host.label}</button>
					: host.status === "connecting" ? <span className="text-xs text-muted-foreground">Connecting</span>
					: <button type="button" className="text-xs text-muted-foreground underline" onClick={() => onStart(host.hostId)}>Start on {host.label}</button>}
			</div>
			{host.status === "connected" && workspaces.filter((workspace) => workspace.hostId === host.hostId).map((workspace) => <div key={workspace.id} className="pl-2 text-sm">
				<div className="truncate text-muted-foreground">{workspace.name}</div>
				{workspace.sessions.map((session) => <button
					key={session.id}
					type="button"
					className="block max-w-full truncate rounded px-2 py-1 text-left hover:bg-interactive-hover"
					data-testid="remote-session-row"
					onClick={() => onOpenSession(host.hostId, workspace.id, session.id)}
				>{session.title}</button>)}
			</div>)}
		</div>)}
	</section>;
}
