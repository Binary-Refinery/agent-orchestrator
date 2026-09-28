import { type InfiniteData, useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useBlocker } from "@tanstack/react-router";
import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";
import { PanelRight } from "lucide-react";
import { mergeConversationPages, toSnapshot } from "../hooks/useConversation";
import { baseUrlForHost, clientForHost, labelForHost, subscribeConnectedHosts } from "../lib/host-clients";
import { apiErrorCode } from "../lib/api-client";
import { refKey } from "../lib/hosts";
import { aoBridge } from "../lib/bridge";
import { chatDraftDialogCopy, confirmDiscardChatDrafts, getChatDraftBoundaries, subscribeChatDraftBoundaries } from "../lib/chat-draft-boundary";
import { useWorkspaceSession, remoteWorkspaceQueryKey } from "../hooks/useWorkspaceQuery";
import { ChatWorkspace } from "./chat/ChatWorkspace";
import { SessionPaneTab } from "./CenterPane";
import { SessionTopbarHost } from "./SessionTopbarPortal";
import { TopbarButton } from "./TopbarButton";
import { RemoteTerminalView } from "./RemoteTerminalView";
import { RemoteSessionInspector } from "./RemoteSessionInspector";
import type { ConversationSnapshot } from "../types/conversation";

const CONVERSATION_PAGE_SIZE = 200;

export function RemoteSessionRoute({ hostId, sessionId }: { hostId: string; sessionId: string }) {
	const uiSessionId = `remote:${refKey({ host: hostId, id: sessionId })}`;
	const draftBoundaries = useSyncExternalStore(subscribeChatDraftBoundaries, () => getChatDraftBoundaries(uiSessionId));
	useBlocker({
		disabled: draftBoundaries.length === 0,
		enableBeforeUnload: draftBoundaries.length > 0,
		shouldBlockFn: () => !confirmDiscardChatDrafts(getChatDraftBoundaries(uiSessionId), (message) => window.confirm(message)),
	});
	useEffect(() => {
		aoBridge.app.setChatDraftRisk?.(draftBoundaries, chatDraftDialogCopy(draftBoundaries));
		return () => aoBridge.app.setChatDraftRisk?.([]);
	}, [draftBoundaries]);
	return <RemoteSessionView hostId={hostId} sessionId={sessionId} />;
}

/** Host-routed data and actions around the same Chat and terminal surfaces as local sessions. */
export function RemoteSessionView({ hostId, sessionId }: { hostId: string; sessionId: string }) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const session = useWorkspaceSession(sessionId, hostId);
	const proxyBase = useSyncExternalStore(subscribeConnectedHosts, () => baseUrlForHost(hostId));
	const hostLabel = useSyncExternalStore(subscribeConnectedHosts, () => labelForHost(hostId)) ?? hostId;
	const sessionRefKey = refKey({ host: hostId, id: sessionId });
	const [refreshErrorKey, setRefreshErrorKey] = useState<string | null>(null);
	const [inspectorOpen, setInspectorOpen] = useState(true);
	const conversationKey = ["remote-conversation", hostId, sessionId] as const;
	const fetchConversationPage = useCallback(async (beforeSequence?: number) => {
		const { data, error } = await clientForHost(hostId).GET("/api/v1/sessions/{sessionId}/conversation", {
			params: { path: { sessionId }, query: { beforeSequence, limit: CONVERSATION_PAGE_SIZE } },
		});
		if (error) throw error;
		return toSnapshot(data);
	}, [hostId, sessionId]);
	const conversation = useInfiniteQuery({
		queryKey: conversationKey,
		enabled: session.data?.mode === "chat" && !!proxyBase,
		staleTime: Infinity, // The timer below refreshes only the live page, not all loaded history.
		initialPageParam: undefined as number | undefined,
		queryFn: ({ pageParam }) => fetchConversationPage(pageParam),
		getNextPageParam: (page) => page.hasMoreBefore ? page.oldestSequence : undefined,
		select: (data) => mergeConversationPages(data.pages),
	});
	const snapshot = conversation.data;
	const refreshConversation = useCallback(async () => {
		if (!proxyBase || session.data?.mode !== "chat") return;
		try {
			const latest = await fetchConversationPage();
			queryClient.setQueryData<InfiniteData<ConversationSnapshot>>(conversationKey, (previous) => {
				if (previous?.pages.length && previous.pages[0].conversationId === latest.conversationId && previous.pages[0].activeBranchId === latest.activeBranchId && latest.latestSequence < previous.pages[0].latestSequence) {
					return previous;
				}
				if (!previous?.pages.length || !latest.hasMoreBefore || previous.pages[0].conversationId !== latest.conversationId || previous.pages[0].activeBranchId !== latest.activeBranchId) {
					return { pages: [latest], pageParams: [undefined] };
				}
				const priorLive = previous.pages[0];
				// Keep only rows that fell out of the bounded live window. A missing row
				// still inside that window was removed/updated, not older history.
				const slidOut = latest.hasMoreBefore && latest.oldestSequence > priorLive.oldestSequence
					? priorLive.items.filter((item) => item.sequence < latest.oldestSequence)
					: [];
				const slidTurnIds = new Set(slidOut.map((item) => item.turnId));
				const live = slidOut.length
					? mergeConversationPages([latest, { ...priorLive, items: slidOut, turns: priorLive.turns.filter((turn) => slidTurnIds.has(turn.id)) }]) ?? latest
					: latest;
				return { ...previous, pages: [live, ...previous.pages.slice(1)] };
			});
			setRefreshErrorKey((current) => current === sessionRefKey ? null : current);
		} catch {
			// A failed follow-up read must not turn an accepted send into a failed send.
			setRefreshErrorKey(sessionRefKey);
		}
	}, [fetchConversationPage, hostId, proxyBase, queryClient, session.data?.mode, sessionId, sessionRefKey]);
	useEffect(() => {
		if (!proxyBase || session.data?.mode !== "chat") return;
		// A full infinite-query refetch would re-download every older page every two seconds.
		const timer = window.setInterval(() => { void refreshConversation(); }, 2_000);
		return () => window.clearInterval(timer);
	}, [proxyBase, refreshConversation, session.data?.mode]);
	const resolve = useMutation({
		mutationFn: async ({ requestId, decisionId }: { requestId: string; decisionId: string }) => {
			const { error } = await clientForHost(hostId).POST("/api/v1/sessions/{sessionId}/conversation/approvals/{requestId}/resolve", {
				params: { path: { sessionId, requestId } },
				body: { decisionId },
			});
			// Another client may have answered while this card was on screen.
			if (error && apiErrorCode(error) !== "CHAT_REQUEST_NOT_PENDING") throw error;
		},
		onSettled: refreshConversation,
	});
	const resolveInput = useMutation({
		mutationFn: async ({ requestId, action, content }: { requestId: string; action: "accept" | "decline" | "cancel"; content?: Record<string, unknown> }) => {
			const { error } = await clientForHost(hostId).POST("/api/v1/sessions/{sessionId}/conversation/inputs/{requestId}/resolve", {
				params: { path: { sessionId, requestId } },
				body: { action, content },
			});
			if (error && apiErrorCode(error) !== "CHAT_REQUEST_NOT_PENDING") throw error;
		},
		onSettled: refreshConversation,
	});
	const send = useMutation({
		mutationFn: async ({ text, attachments, clientMessageId }: { text: string; attachments?: { mimeType: string; data: string }[]; clientMessageId: string }) => {
			const { error } = await clientForHost(hostId).POST("/api/v1/sessions/{sessionId}/conversation/messages", {
				params: { path: { sessionId } },
				body: { text, clientMessageId, ...(attachments?.length ? { attachments } : {}) },
			});
			if (error) throw error;
		},
		onSuccess: refreshConversation,
	});
	const interrupt = useMutation({
		mutationFn: async () => {
			const { error } = await clientForHost(hostId).POST("/api/v1/sessions/{sessionId}/conversation/interrupt", {
				params: { path: { sessionId } },
			});
			if (error) throw error;
		},
		onSettled: refreshConversation,
	});
	const stop = useMutation({
		mutationFn: async () => {
			const { error } = await clientForHost(hostId).POST("/api/v1/sessions/{sessionId}/kill", {
				params: { path: { sessionId } },
			});
			if (error) throw error;
		},
		onSuccess: () => queryClient.invalidateQueries({ queryKey: remoteWorkspaceQueryKey(hostId) }),
	});
	const title = session.data?.title ?? sessionId;
	const hostActions = <div className="flex items-center gap-3">
		<span className="max-w-40 truncate text-xs text-muted-foreground" title={hostId}>{hostLabel}</span>
		<TopbarButton variant="kill" disabled={!session.data || session.data.isTerminated || stop.isPending || !proxyBase} onClick={() => {
			if (window.confirm(t("remote.confirmStop", { title, hostId }))) stop.mutate();
		}}>{t("remote.stopSession")}</TopbarButton>
		<TopbarButton
			aria-label={inspectorOpen ? t("shell.closeInspector") : t("shell.openInspector")}
			aria-pressed={inspectorOpen}
			onClick={() => setInspectorOpen((open) => !open)}
			variant="icon"
		><PanelRight aria-hidden="true" className="size-icon-md" /></TopbarButton>
	</div>;

	return <div className="relative flex h-full min-h-0 bg-background text-foreground" data-testid="remote-session-view" data-host-id={hostId}>
		<div className="flex min-w-0 flex-1 flex-col">
		{session.data?.mode === "chat" && <SessionTopbarHost className="relative z-chrome flex h-inspector-tabs w-full shrink-0 overflow-hidden" data-testid="session-topbar-host" />}
		{session.isError && <p role="alert" className="px-4 py-2 text-sm text-destructive">{t("remote.loadSessionFailed")}</p>}
		{stop.isError && <p role="alert" className="px-4 py-2 text-sm text-destructive">{t("remote.stopSessionFailed")}</p>}
		{!proxyBase && <p role="alert" className="px-4 py-2 text-sm text-destructive">{t("remote.loadSessionFailed")}</p>}
		{(conversation.isError || refreshErrorKey === sessionRefKey) && <p role="alert" className="px-4 py-2 text-sm text-destructive">{t("remote.loadConversationFailed")}</p>}
		<div className="min-h-0 flex-1">
			{proxyBase && session.data?.mode === "chat" && snapshot ? <ChatWorkspace
				assetBaseUrl={proxyBase}
				busy={resolve.isPending || resolveInput.isPending || send.isPending}
				commandError={send.isError ? t("remote.sendFailed") : interrupt.isError ? t("remote.stopSessionFailed") : resolve.isError || resolveInput.isError ? t("inspector.resolveReviewFailed") : undefined}
				headerActions={hostActions}
				hasOlder={conversation.hasNextPage}
				loadingOlder={conversation.isFetchingNextPage}
				newWorkDisabled={session.data.isTerminated}
				onDecide={(requestId, decisionId) => resolve.mutate({ requestId, decisionId })}
				onInterrupt={() => interrupt.mutate()}
				onLoadOlder={() => { void conversation.fetchNextPage(); }}
				onResolveInput={(requestId, action, content) => resolveInput.mutateAsync({ requestId, action, content })}
				onSend={(text, attachments, clientMessageId) => send.mutateAsync({ text, attachments, clientMessageId: clientMessageId ?? crypto.randomUUID() })}
				sendPending={send.isPending}
				session={session.data}
				sessionTitle={title}
				snapshot={snapshot}
				uiSessionId={`remote:${sessionRefKey}`}
			/> : proxyBase && session.data?.mode === "tui" ? <div className="flex h-full min-h-0 flex-col">
				<header className="flex h-inspector-tabs shrink-0 items-stretch justify-between bg-sidebar">
					<div role="tablist"><SessionPaneTab isActive label={title} session={session.data} /></div>
					{hostActions}
				</header>
				<div className="min-h-0 flex-1"><RemoteTerminalView hostId={hostId} proxyBase={proxyBase} terminalHandleId={session.data.terminalHandleId ?? sessionId} terminalGeneration={session.data.terminalGeneration} /></div>
			</div> : proxyBase && (session.isLoading || conversation.isLoading) ? <div className="grid h-full place-items-center text-sm text-muted-foreground">{t("remote.loadingSession")}</div>
				: proxyBase && !session.data && !session.isError ? <div className="grid h-full place-items-center text-sm text-muted-foreground">{t("session.notFound")}</div>
				: null}
		</div>
		</div>
		{session.data && inspectorOpen ? <div className="w-[min(20rem,40%)] shrink-0 overflow-hidden border-l border-border-strong bg-background 2xl:w-[min(24rem,40%)]" data-testid="panel-inspector">
			<RemoteSessionInspector key={sessionRefKey} connected={Boolean(proxyBase)} hostId={hostId} hostLabel={hostLabel} session={session.data} />
		</div> : null}
	</div>;
}
