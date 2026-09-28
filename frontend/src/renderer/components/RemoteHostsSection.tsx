import { Fragment, useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangle, Folder, FolderOpen, Plus } from "lucide-react";
import type { RemoteHost } from "../hooks/useRemoteHosts";
import { getSessionStatusDotView } from "../lib/session-presentation";
import { cn } from "../lib/utils";
import { sortedWorkerSessions, type WorkspaceSummary } from "../types/workspace";
import { Badge } from "./ui/badge";
import { SidebarMenuButton, SidebarMenuItem, SidebarMenuSub, SidebarMenuSubItem } from "./ui/sidebar";

type Props = {
	hosts: RemoteHost[];
	workspaces: WorkspaceSummary[];
	failedHostIds?: string[];
	activeHostId?: string;
	activeProjectId?: string;
	activeSessionId?: string;
	onOpenSession: (hostId: string, projectId: string, sessionId: string) => void;
	onStart: (hostId: string) => void;
	onRetry: () => void;
};

function RemoteProjectRow({ host, workspace, activeProjectId, activeSessionId, onOpenSession }: {
	host: RemoteHost;
	workspace: WorkspaceSummary;
	activeProjectId?: string;
	activeSessionId?: string;
	onOpenSession: Props["onOpenSession"];
}) {
	const { t } = useTranslation();
	const active = activeProjectId === workspace.id;
	const [expanded, setExpanded] = useState(true);
	const sessions = sortedWorkerSessions(workspace.sessions).filter((session) => session.isTerminated !== true);
	return <SidebarMenuItem data-remote-project-row="" data-host-id={host.hostId} data-project-id={workspace.id}>
		<SidebarMenuButton
			aria-expanded={expanded}
			aria-label={t("shell.toggleProject", { name: `${workspace.name} · ${host.label}` })}
			className="h-9 gap-2 rounded-lg px-2.5 text-sm font-medium text-muted-foreground hover:bg-interactive-hover hover:text-foreground group-data-[collapsible=icon]:size-control-board! group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:p-0! [&_svg]:size-icon-md"
			onClick={() => setExpanded((open) => !open)}
			tooltip={`${workspace.name} · ${host.label}`}
		>
			{expanded ? <FolderOpen aria-hidden="true" strokeWidth={1.75} /> : <Folder aria-hidden="true" strokeWidth={1.75} />}
			<span className="sidebar-expanded-chrome min-w-0 flex-1 truncate group-data-[collapsible=icon]:hidden">{workspace.name}</span>
			<Badge variant="outline" className="sidebar-expanded-chrome h-4 shrink-0 px-1.5 text-2xs group-data-[collapsible=icon]:hidden">{host.label}</Badge>
		</SidebarMenuButton>
		{expanded && sessions.length > 0 && <SidebarMenuSub className="sidebar-expanded-chrome mx-0 ml-3.5 translate-x-0 gap-px border-l-0 px-0 py-1 group-data-[collapsible=icon]:hidden">
			{sessions.map((session) => {
				const activeSession = active && activeSessionId === session.id;
				const dot = getSessionStatusDotView(session);
				return <SidebarMenuSubItem key={session.id} className="pl-0.5" data-session-row="">
					<SidebarMenuButton
						aria-current={activeSession ? "page" : undefined}
						aria-label={t("shell.openSession", { title: session.title })}
						className="h-8 gap-1.5 rounded-lg pl-1.5 pr-2.5 text-left text-sm hover:bg-interactive-hover hover:text-foreground"
						data-testid="remote-session-row"
						isActive={activeSession}
						onClick={() => onOpenSession(host.hostId, workspace.id, session.id)}
					>
						<span aria-hidden="true" className="inline-flex shrink-0 px-1.5"><span className={cn("size-2 rounded-full", dot.className, dot.breathe && "animate-status-pulse")} /></span>
						<span className="min-w-0 truncate">{session.title}</span>
					</SidebarMenuButton>
				</SidebarMenuSubItem>;
			})}
		</SidebarMenuSub>}
	</SidebarMenuItem>;
}

export function RemoteHostsSection({ hosts, workspaces, failedHostIds = [], activeHostId, activeProjectId, activeSessionId, onOpenSession, onStart, onRetry }: Props) {
	const { t } = useTranslation();
	if (hosts.length === 0) return null;
	return <>
		{hosts.map((host) => {
			if (host.status !== "connected") return <SidebarMenuItem key={host.url} data-host-id={host.hostId}>
				<SidebarMenuButton
					aria-label={host.status === "offline" ? t("remote.retryHost", { label: host.label }) : undefined}
					className="h-9 gap-2 rounded-lg px-2.5 text-sm text-muted-foreground hover:bg-interactive-hover [&_svg]:size-icon-md"
					disabled={host.status === "connecting"}
					onClick={onRetry}
					title={host.status === "offline" ? t("remote.hostOffline") : undefined}
				>
					{host.status === "offline" ? <AlertTriangle aria-hidden="true" /> : <Folder aria-hidden="true" />}
					<span className="truncate">{host.label}</span>
					<span className="ml-auto text-xs">{host.status === "connecting" ? t("terminal.connecting") : t("remote.retryHost", { label: host.label })}</span>
				</SidebarMenuButton>
			</SidebarMenuItem>;
			const projects = workspaces.filter((workspace) => workspace.hostId === host.hostId);
			return <Fragment key={host.url}>
				{projects.map((workspace) => <RemoteProjectRow
					key={`${host.hostId}:${workspace.id}`}
					host={host}
					workspace={workspace}
					activeProjectId={activeHostId === host.hostId ? activeProjectId : undefined}
					activeSessionId={activeHostId === host.hostId ? activeSessionId : undefined}
					onOpenSession={onOpenSession}
				/>)}
				<SidebarMenuItem>
					<SidebarMenuButton aria-label={t("remote.startOn", { label: host.label })} className="h-8 gap-2 rounded-lg px-2.5 text-sm text-muted-foreground hover:bg-interactive-hover hover:text-foreground [&_svg]:size-icon-md" onClick={() => onStart(host.hostId)}>
						<Plus aria-hidden="true" />
						<span className="truncate">{t("remote.startOn", { label: host.label })}</span>
					</SidebarMenuButton>
				</SidebarMenuItem>
				{failedHostIds.includes(host.hostId) && <SidebarMenuItem>
					<SidebarMenuButton className="h-8 rounded-lg px-2.5 text-left text-xs text-destructive hover:bg-interactive-hover" onClick={onRetry}>{t("remoteHosts.loadFailed")}</SidebarMenuButton>
				</SidebarMenuItem>}
			</Fragment>;
		})}
	</>;
}
