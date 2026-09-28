import { useQuery } from "@tanstack/react-query";
import {
	InspectorActivityTimelineView,
	InspectorPullRequestCardView,
	InspectorSection,
	SessionInspectorShellView,
	SessionInspectorSummaryView,
	inspectorEmptyClass,
	type InspectorPullRequest,
	type InspectorView,
} from "@aoagents/product-ui";
import { ArrowLeft, ArrowUpRight, Files, GitPullRequest, List } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { formatTimeCompact } from "../lib/format-time";
import { clientForHost } from "../lib/host-clients";
import { prBrowserUrl, prCardPresentation, prNounKeys, sessionPRDisplaySummaries } from "../lib/pr-display";
import { statusLabel, statusTone } from "../lib/workspace-file-status";
import type { WorkspaceSession } from "../types/workspace";
import { ProductExternalLink } from "./ProductExternalLink";
import { WorkspaceEntryIcon } from "./WorkspaceEntryIcon";

/** Read-only host-routed inspector. Local-only review, browser and editor actions are deliberately absent. */
export function RemoteSessionInspector({ connected, hostId, hostLabel, session }: {
	connected: boolean;
	hostId: string;
	hostLabel: string;
	session: WorkspaceSession;
}) {
	const { t } = useTranslation();
	const [view, setView] = useState<"summary" | "files">("summary");
	const [selectedPath, setSelectedPath] = useState<string | null>(null);
	const prs = sessionPRDisplaySummaries(session);
	const files = useQuery({
		queryKey: ["remote-workspace-files", hostId, session.id],
		queryFn: async () => {
			const { data, error } = await clientForHost(hostId).GET("/api/v1/sessions/{sessionId}/workspace/files", {
				params: { path: { sessionId: session.id } },
			});
			if (error) throw error;
			return data;
		},
		enabled: connected && view === "files",
		refetchInterval: view === "files" ? 5_000 : false,
	});
	const file = useQuery({
		queryKey: ["remote-workspace-file", hostId, session.id, selectedPath],
		queryFn: async () => {
			const { data, error } = await clientForHost(hostId).GET("/api/v1/sessions/{sessionId}/workspace/file", {
				params: { path: { sessionId: session.id }, query: { path: selectedPath! } },
			});
			if (error) throw error;
			return data;
		},
		enabled: connected && view === "files" && selectedPath !== null,
		refetchInterval: view === "files" ? 5_000 : false,
	});
	const tabs = [
		{ id: "summary" as const, label: t("inspector.summary"), icon: <List aria-hidden="true" /> },
		{ id: "files" as const, label: t("inspector.files"), icon: <Files aria-hidden="true" /> },
	];
	return <div className="session-inspector contents" data-testid="remote-session-inspector">
		<SessionInspectorShellView
			activeView={view}
			ariaLabel={t("inspector.aria")}
			browserPoppedOut={false}
			onViewChange={(next: InspectorView) => setView(next === "files" ? "files" : "summary")}
			tabs={tabs}
			summaryView={<SessionInspectorSummaryView
			activityTitle={t("inspector.activity")}
			activity={<InspectorActivityTimelineView events={[
				{ tone: "neutral", content: t("inspector.timeline.createdWorkspace"), timestamp: formatTimeCompact(session.createdAt) },
				{ tone: "now", content: session.displayStatus ?? session.status, timestamp: formatTimeCompact(session.updatedAt) },
			]} />}
			context={<InspectorSection title={t("inspector.overview")}>
				<dl className="space-y-2 py-1 text-xs">
					<div className="flex justify-between gap-2"><dt className="text-settings-muted">{t("remote.host")}</dt><dd className="truncate text-settings-label" title={hostId}>{hostLabel}</dd></div>
					<div className="flex justify-between gap-2"><dt className="text-settings-muted">{t("createProject.project")}</dt><dd className="truncate text-settings-label" title={session.workspaceName}>{session.workspaceName}</dd></div>
					<div className="flex justify-between gap-2"><dt className="text-settings-muted">{t("inspector.agent")}</dt><dd className="truncate text-settings-label">{session.provider}</dd></div>
					{session.branch && <div className="flex justify-between gap-2"><dt className="text-settings-muted">{t("inspector.branch")}</dt><dd className="truncate font-mono text-settings-label" title={session.branch}>{session.branch}</dd></div>}
				</dl>
			</InspectorSection>}
			pullRequestTitle={prs.length > 1 ? t("inspector.pullRequests", { count: prs.length }) : t("inspector.pullRequest")}
			pullRequestCards={prs.length ? prs.map((pr) => {
				const card: InspectorPullRequest = {
					...pr,
					card: prCardPresentation(pr),
					href: prBrowserUrl(pr),
					stateLabel: t(`pr.state.${pr.state}`),
				};
				return <InspectorPullRequestCardView
					countNounLabel={(count, noun) => `${count} ${t(prNounKeys[noun], { count })}`}
					externalIcon={<ArrowUpRight aria-hidden="true" className="size-icon-2xs shrink-0" />}
					externalLink={ProductExternalLink}
					key={pr.url || pr.number}
					openLabel={t("inspector.openPR", { number: pr.number })}
					pr={card}
					pullRequestIcon={<GitPullRequest aria-hidden="true" className="size-icon-sm shrink-0" />}
				/>;
			}) : <p className={inspectorEmptyClass}>{t("inspector.noPROpened")}</p>}
		/>}
		filesView={<div role="tabpanel" className="board-scrollbar h-full overflow-y-auto p-3">
			{!connected ? <p className={inspectorEmptyClass}>{t("remote.hostOffline")}</p>
				: selectedPath ? <>
					<button type="button" className="mb-3 inline-flex items-center gap-1.5 text-xs text-settings-muted hover:text-foreground" onClick={() => setSelectedPath(null)}>
						<ArrowLeft aria-hidden="true" className="size-icon-sm" />{t("files.explorer.backToTree")}
					</button>
					<p className="mb-2 break-all font-mono text-xs text-settings-label">{selectedPath}</p>
					{file.isPending ? <p className={inspectorEmptyClass}>{t("files.loadingDiff")}</p>
						: file.isError ? <p role="alert" className="text-xs text-destructive">{t("files.error.loadWorkspaceFile")}</p>
						: file.data?.binary ? <p className={inspectorEmptyClass}>{t("files.binaryUnavailable")}</p>
						: file.data?.contentTruncated || file.data?.diffTruncated ? <p className={inspectorEmptyClass}>{t("inspector.filesUnavailable")}</p>
						: <pre className="overflow-x-auto whitespace-pre-wrap break-words font-mono text-2xs text-settings-label">{file.data?.diff || file.data?.content}</pre>}
				</> : files.isPending ? <p className={inspectorEmptyClass}>{t("files.loadingDiff")}</p>
					: files.isError ? <p role="alert" className="text-xs text-destructive">{t("files.error.loadWorkspaceFile")}</p>
					: !files.data?.files.length ? <p className={inspectorEmptyClass}>{t("files.explorer.empty")}</p>
					: <div className="space-y-0.5">{files.data.files.map((entry) => <button
						key={entry.path}
						type="button"
						className="flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs text-settings-label hover:bg-interactive-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
						onClick={() => setSelectedPath(entry.path)}
					>
						<WorkspaceEntryIcon kind="file" name={entry.path.split("/").pop() ?? entry.path} className="size-icon-sm" />
						<span className="min-w-0 flex-1 truncate" title={entry.path}>{entry.path}</span>
						<span className={statusTone[entry.status]}>{statusLabel[entry.status]}</span>
					</button>)}</div>}
		</div>}
		/>
	</div>;
}
