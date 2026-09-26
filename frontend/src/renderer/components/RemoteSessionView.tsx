import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef, useState, useSyncExternalStore, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { baseUrlForHost, clientForHost, subscribeConnectedHosts } from "../lib/host-clients";
import { useWorkspaceSession, remoteWorkspaceQueryKey } from "../hooks/useWorkspaceQuery";
import { RemoteTerminalView } from "./RemoteTerminalView";

/** A safe remote surface until native desktop actions have host-aware implementations. */
export function RemoteSessionView({ hostId, sessionId }: { hostId: string; sessionId: string }) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const session = useWorkspaceSession(sessionId, hostId);
	const [message, setMessage] = useState("");
	const deliveryId = useRef<string | null>(null);
	const conversationKey = ["remote-conversation", hostId, sessionId] as const;
	const conversation = useQuery({
		queryKey: conversationKey,
		enabled: session.data?.mode === "chat",
		refetchInterval: 2_000,
		queryFn: async () => {
			const { data, error } = await clientForHost(hostId).GET("/api/v1/sessions/{sessionId}/conversation", {
				params: { path: { sessionId } },
			});
			if (error) throw error;
			return data;
		},
	});
	const send = useMutation({
		mutationFn: async ({ text, id }: { text: string; id: string }) => {
			const { error } = await clientForHost(hostId).POST("/api/v1/sessions/{sessionId}/conversation/messages", {
				params: { path: { sessionId } },
				body: { text, clientMessageId: id },
			});
			if (error) throw error;
		},
		onSuccess: async () => {
			setMessage("");
			deliveryId.current = null;
			await queryClient.invalidateQueries({ queryKey: conversationKey });
		},
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
	const onSubmit = (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		if (message.trim() && !send.isPending) {
			deliveryId.current ??= crypto.randomUUID();
			send.mutate({ text: message.trim(), id: deliveryId.current });
		}
	};
	const title = session.data?.title ?? sessionId;
	const proxyBase = useSyncExternalStore(subscribeConnectedHosts, () => baseUrlForHost(hostId));
	return <div className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-6" data-testid="remote-session-view" data-host-id={hostId}>
		<header className="flex items-start justify-between gap-4">
			<div>
				<p className="text-xs text-muted-foreground">{t("remote.hostLabel", { hostId })}</p>
				<h1 className="text-xl font-semibold">{title}</h1>
				<p className="text-sm text-muted-foreground">{session.data?.status ?? t("remote.loadingSession")}</p>
			</div>
			<button type="button" className="rounded-md border px-3 py-1.5 text-sm" disabled={!session.data || session.data.isTerminated || stop.isPending} onClick={() => {
			if (window.confirm(t("remote.confirmStop", { title, hostId }))) stop.mutate();
			}}>{t("remote.stopSession")}</button>
		</header>
		{session.isError && <p role="alert">{t("remote.loadSessionFailed")}</p>}
		{stop.isError && <p role="alert">{t("remote.stopSessionFailed")}</p>}
		{session.data?.mode === "chat" ? <>
			<div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto" aria-label={t("remote.conversation")}>
				{conversation.data?.messages.map((item) => <div key={item.id} className="rounded-lg border p-3">
					<p className="text-xs text-muted-foreground">{item.role}</p>
					<p className="whitespace-pre-wrap">{item.text}</p>
				</div>)}
				{conversation.isError && <p role="alert">{t("remote.loadConversationFailed")}</p>}
			</div>
			<form className="flex gap-2" onSubmit={onSubmit}>
				<input className="min-w-0 flex-1 rounded-md border bg-background p-2" aria-label={t("remote.message")} value={message} onChange={(event) => { deliveryId.current = null; setMessage(event.target.value); }} />
				<button type="submit" className="rounded-md border px-4" disabled={!message.trim() || send.isPending || session.data.isTerminated}>{t("browser.annotationSend")}</button>
			</form>
			{send.isError && <p role="alert">{t("remote.sendFailed")}</p>}
		</> : session.data && proxyBase ? (
			<RemoteTerminalView hostId={hostId} proxyBase={proxyBase} terminalHandleId={session.data.terminalHandleId ?? sessionId} />
		) : null}
		<p className="text-xs text-muted-foreground">{t("remote.limitations")}</p>
	</div>;
}
