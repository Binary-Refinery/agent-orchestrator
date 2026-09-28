import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
	SessionCardView,
	SessionsArchiveView,
	SessionsBoardGridView,
	archiveToggleOffsetClassName,
	type ProductUITranslator,
} from "@aoagents/product-ui";
import { AlertTriangle, Archive, LayoutDashboard, Loader2, RotateCcw, RotateCw } from "lucide-react";
import { useRef, useState, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";
import type { MessageKey } from "../i18n";
import { useRemoteProjectQuery, remoteWorkspaceQueryKey } from "../hooks/useWorkspaceQuery";
import { apiErrorCode, apiErrorMessage } from "../lib/api-client";
import { useShell } from "../lib/shell-context";
import { boardKanbanColumnOrder, getKanbanColumnView } from "../lib/session-presentation";
import { clientForHost, connectedHosts, labelForHost, subscribeConnectedHosts } from "../lib/host-clients";
import { openRemoteOrchestrator } from "../lib/remote-orchestrator";
import { isChatPreflightCode } from "../lib/spawn-orchestrator";
import { formatTimeCompact } from "../lib/format-time";
import { prBrowserUrl, sessionPRDisplaySummaries } from "../lib/pr-display";
import { useUiStore } from "../stores/ui-store";
import {
	hasConfiguredOrchestratorAgent,
	newestActiveOrchestrator,
	orchestratorHealth,
	workerSessions,
	type WorkspaceSession,
} from "../types/workspace";
import { AgentAvatar } from "./AgentAvatar";
import { ProjectBoardEmpty } from "./BoardEmptyStates";
import { ProductExternalLink } from "./ProductExternalLink";
import { ProjectBoardActions } from "./ProjectBoardActions";
import { SessionArchiveDialog } from "./SessionArchiveDialog";
import { sessionsBoardLabels, toBoardSessionPresentation } from "./SessionsBoardAdapters";
import { TopbarButton, topbarProjectLabelClass } from "./TopbarButton";
import { cn } from "../lib/utils";

/** The remote board uses the same presentation as the local board, but every
 * write and navigation target includes the host. */
export function RemoteProjectBoard({ hostId, projectId }: { hostId: string; projectId: string }) {
	const { t } = useTranslation();
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const { openRemoteProjectSettings } = useShell();
	const project = useRemoteProjectQuery(hostId, projectId);
	const connected = useSyncExternalStore(subscribeConnectedHosts, connectedHosts).includes(hostId);
	const hostLabel = labelForHost(hostId) ?? hostId;
	const requestNewTask = useUiStore((state) => state.requestNewTask);
	const [spawnError, setSpawnError] = useState("");
	const [spawnErrorCode, setSpawnErrorCode] = useState<string>();
	const [spawning, setSpawning] = useState(false);
	const [restarting, setRestarting] = useState(false);
	const launching = useRef(false);
	const workspace = project.data;
	const orchestrator = newestActiveOrchestrator(workspace?.sessions ?? []);
	const health = workspace ? orchestratorHealth(workspace, restarting) : { state: "ok" as const };
	const workers = workerSessions(workspace?.sessions ?? []);
	const isArchived = (session: WorkspaceSession) =>
		session.kanbanColumn === "archive" || session.isTerminated === true || session.status === "terminated";
	const active = workers.filter((session) => !isArchived(session));
	const archived = workers.filter(isArchived).sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
	const openSession = (sessionId: string) => void navigate({
		to: "/host/$hostId/project/$projectId/session/$sessionId",
		params: { hostId, projectId, sessionId },
	});
	const openOrchestrator = async (mode?: "tui", clean = false) => {
		if (!connected || launching.current || !workspace) return;
		setSpawnError("");
		setSpawnErrorCode(undefined);
		if (!orchestrator && !hasConfiguredOrchestratorAgent(workspace)) {
			openRemoteProjectSettings(hostId, projectId);
			return;
		}
		launching.current = true;
		setSpawning(true);
		setRestarting(clean);
		try {
			const sessionId = await openRemoteOrchestrator(hostId, projectId, orchestrator, mode, clean);
			await queryClient.invalidateQueries({ queryKey: remoteWorkspaceQueryKey(hostId) });
			openSession(sessionId);
		} catch (error) {
			setSpawnErrorCode(apiErrorCode(error));
			setSpawnError(error instanceof Error ? error.message : apiErrorMessage(error, t("shell.couldNotSpawn")));
		} finally {
			launching.current = false;
			setSpawning(false);
			setRestarting(false);
		}
	};
	const actions = {
		orchestrator,
		isSpawning: spawning,
		isProjectRestarting: restarting,
		isProvisioning: false,
		spawnError,
		canCreateAsTui: isChatPreflightCode(spawnErrorCode),
		openNewTask: () => requestNewTask(projectId, hostId),
		openOrchestrator: (mode?: "tui") => { void openOrchestrator(mode); },
	};
	const renderCard = (session: WorkspaceSession) => <RemoteBoardSessionCard
		key={`${hostId}:${session.id}`}
		archived={isArchived(session)}
		connected={connected}
		hostId={hostId}
		onOpen={() => openSession(session.id)}
		session={session}
	/>;
	const boardActions = connected && workspace ? <ProjectBoardActions actions={actions} placement="header" quiet={active.length === 0} /> : null;

	return <div className="relative flex h-full min-h-0 flex-col bg-background text-foreground" data-testid="remote-project-board" data-host-id={hostId} data-project-id={projectId}>
		<div className="workspace-topbar-container center-panel-titlebar flex h-toolbar shrink-0 items-center gap-2 border-b border-border-strong pr-1">
			<span className={cn(topbarProjectLabelClass, "inline-flex min-w-0 items-center gap-1.5")}>
				<LayoutDashboard aria-hidden="true" className="size-icon-md" />
				<span className="truncate">{t("shell.board")}</span>
				<span className="truncate text-muted-foreground">· {hostLabel}</span>
			</span>
			<div className="min-w-0 flex-1" />
			{boardActions ? <div className="workspace-topbar-actions flex shrink-0 items-center">{boardActions}</div> : null}
		</div>
		{!connected ? <p role="alert" className="px-4 py-3 text-sm text-destructive">{t("remote.hostOffline")}</p> : null}
		{project.isError ? <p role="alert" className="px-4 py-3 text-sm text-destructive">{t("shell.couldNotLoadProjects")}</p> : null}
		{project.isSuccess && !workspace ? <p role="alert" className="px-4 py-3 text-sm text-destructive">{t("session.notFound")}</p> : null}
		{connected && workspace && !orchestrator && !hasConfiguredOrchestratorAgent(workspace) ? <div className="mx-3 my-3 flex items-center gap-3 rounded-md border border-border bg-surface px-3 py-2 text-xs text-muted-foreground">
			<span className="min-w-0 flex-1">{t("remote.configureOrchestratorFirst", { label: hostLabel, defaultValue: "Choose an orchestrator agent for this project on {{label}} first." })}</span>
			<button type="button" className="shrink-0 rounded-md px-2 py-1 font-medium text-foreground hover:bg-interactive-hover focus-visible:outline-2 focus-visible:outline-ring" onClick={() => openRemoteProjectSettings(hostId, projectId)}>{t("restoreUnavailable.configureOrchestrator")}</button>
		</div> : null}
		{connected && workspace && health.state !== "ok" && (health.state !== "missing" || hasConfiguredOrchestratorAgent(workspace)) ? <div role="status" className="mx-3 my-3 flex items-center gap-3 rounded-md border border-border bg-surface px-3 py-2 text-xs text-muted-foreground">
			<AlertTriangle aria-hidden="true" className="size-icon-base shrink-0 text-warning" />
			<span className="min-w-0 flex-1">{health.message}</span>
			{health.state === "restart_needed" || health.state === "duplicates" ? <TopbarButton disabled={spawning} onClick={() => { void openOrchestrator(undefined, true); }} variant="primary">
				<RotateCw aria-hidden="true" className="size-3.5" />{t("shell.restart")}
			</TopbarButton> : null}
		</div> : null}
		{workspace?.folderMissing ? <div role="alert" className="mx-3 my-3 flex items-center gap-3 rounded-md border border-border bg-surface px-3 py-2 text-xs text-muted-foreground">
			<AlertTriangle aria-hidden="true" className="size-icon-base shrink-0 text-warning" />
			<span className="min-w-0 flex-1">{t("home.folderMissing")}</span>
		</div> : null}
		{workspace ? <>
			<div className={cn("min-h-0 flex-1 overflow-hidden", archived.length > 0 && archiveToggleOffsetClassName)}>
				{active.length === 0 && connected ? <ProjectBoardEmpty actions={<ProjectBoardActions actions={actions} placement="empty" />} /> :
					<SessionsBoardGridView
						columns={boardKanbanColumnOrder.map((column) => getKanbanColumnView(column, t))}
						labels={sessionsBoardLabels(t)}
						renderSessionCard={renderCard}
						sessions={active}
					/>}
			</div>
			<SessionsArchiveView
				labels={{ archive: t("shell.archive"), archiveAria: t("shell.archiveSessionsAria", { count: archived.length }), archivedSessions: t("shell.archivedSessions") }}
				renderSessionCard={renderCard}
				resetKey={`${hostId}:${projectId}`}
				sessions={archived}
			/>
		</> : null}
	</div>;
}

function RemoteBoardSessionCard({ archived, connected, hostId, onOpen, session }: {
	archived: boolean;
	connected: boolean;
	hostId: string;
	onOpen: () => void;
	session: WorkspaceSession;
}) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const [confirmOpen, setConfirmOpen] = useState(false);
	const [pending, setPending] = useState(false);
	const [error, setError] = useState("");
	const prs = sessionPRDisplaySummaries(session);
	const translate: ProductUITranslator = (key, values) => t(key as MessageKey, values);
	const updateSession = async (action: "kill" | "restore") => {
		if (!connected || pending) return;
		setPending(true);
		setError("");
		try {
			const client = clientForHost(hostId);
			const { error: apiError } = action === "kill"
				? await client.POST("/api/v1/sessions/{sessionId}/kill", { params: { path: { sessionId: session.id } } })
				: await client.POST("/api/v1/sessions/{sessionId}/restore", { params: { path: { sessionId: session.id } } });
			if (apiError) throw apiError;
			await queryClient.invalidateQueries({ queryKey: remoteWorkspaceQueryKey(hostId) });
		} catch (cause) {
			setError(apiErrorMessage(cause, action === "kill" ? t("remote.stopSessionFailed") : t("terminal.unableRestore")));
		} finally {
			setPending(false);
		}
	};
	const cardAction = archived ? <button
		aria-label={`Restore ${session.title}`}
		className="inline-flex h-7 items-center gap-1 rounded-md px-1.5 text-xs text-muted-foreground hover:bg-interactive-hover hover:text-foreground disabled:opacity-50"
		disabled={!connected || pending}
		onClick={(event) => { event.stopPropagation(); void updateSession("restore"); }}
		type="button"
	>{pending ? <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> : <RotateCcw aria-hidden="true" className="size-3.5" />}{t("shell.restoreSession")}</button> : undefined;
	const archiveControl = !archived ? <SessionArchiveDialog
		open={confirmOpen}
		onOpenChange={setConfirmOpen}
		onConfirm={() => { setConfirmOpen(false); void updateSession("kill"); }}
		session={session}
		trigger={<button
			aria-label={t("shell.archiveNamed", { title: session.title })}
			className="inline-flex size-control-md items-center justify-center rounded-sm text-passive hover:bg-interactive-hover hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60 disabled:opacity-50"
			disabled={!connected || pending}
			onClick={(event) => { event.stopPropagation(); setConfirmOpen(true); }}
			type="button"
		>{pending ? <Loader2 aria-hidden="true" className="size-icon-sm animate-spin" /> : <Archive aria-hidden="true" className="size-icon-sm" />}</button>}
	/> : undefined;
	return <SessionCardView
		action={cardAction}
		error={error || undefined}
		externalLink={ProductExternalLink}
		labels={{
			formatTime: formatTimeCompact,
			intakeIssue: (id) => t("shell.intakeIssue", { id }),
			pr: { short: t("pr.short"), states: {
				closed: t("pr.state.closed"), draft: t("pr.state.draft"),
				merged: t("pr.state.merged"), open: t("pr.state.open"),
			} },
			updatedAt: (timestamp) => t("shell.lastMessageAt", { time: formatTimeCompact(timestamp) }),
		}}
		onOpen={onOpen}
		overlay={archiveControl}
		prs={prs.map((pr) => ({ number: pr.number, state: pr.state, url: prBrowserUrl(pr) }))}
		renderAvatar={(provider) => <AgentAvatar provider={provider} />}
		session={toBoardSessionPresentation(session, t)}
		translate={translate}
	/>;
}
