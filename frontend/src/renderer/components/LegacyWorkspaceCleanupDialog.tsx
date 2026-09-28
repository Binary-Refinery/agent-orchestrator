import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { apiClient, apiErrorMessage, getApiBaseUrl, subscribeApiBaseUrl } from "../lib/api-client";
import { usesPreviewWorkspaceData } from "../lib/preview-mode";
import { useUiStore } from "../stores/ui-store";
import { ConfirmDialog } from "./ConfirmDialog";

const STORAGE_KEY = "ao.legacyWorkspaceCleanupPrompt.dismissed.v1";
const PROMPT_THRESHOLD_BYTES = 1 << 30;
const previewQueryKey = ["legacy-workspace-cleanup-preview"] as const;

function readDismissed(): boolean {
	try {
		return window.localStorage.getItem(STORAGE_KEY) === "true";
	} catch {
		return false;
	}
}

function formatBytes(bytes: number): string {
	const units = ["B", "KB", "MB", "GB", "TB"];
	let value = Math.max(0, bytes);
	let unit = 0;
	while (value >= 1024 && unit < units.length - 1) {
		value /= 1024;
		unit += 1;
	}
	const digits = value >= 10 || unit === 0 ? 0 : 1;
	return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: digits }).format(value)} ${units[unit]}`;
}

export function LegacyWorkspaceCleanupDialog() {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const showGlobalToast = useUiStore((state) => state.showGlobalToast);
	const apiBaseUrl = useSyncExternalStore(subscribeApiBaseUrl, getApiBaseUrl, () => "");
	const [dismissed, setDismissed] = useState(readDismissed);
	const [open, setOpen] = useState(false);

	const preview = useQuery({
		queryKey: previewQueryKey,
		queryFn: async () => {
			const { data, error } = await apiClient.GET("/api/v1/sessions/cleanup/preview");
			if (error || !data) throw new Error(apiErrorMessage(error, t("legacyCleanup.previewFailed")));
			return data;
		},
		enabled: !dismissed && !usesPreviewWorkspaceData && Boolean(apiBaseUrl),
		refetchOnWindowFocus: false,
		retry: false,
		staleTime: Number.POSITIVE_INFINITY,
	});

	const finishPrompt = useCallback(() => {
		setOpen(false);
		setDismissed(true);
		try {
			window.localStorage.setItem(STORAGE_KEY, "true");
		} catch {
			// Dismissing this informational prompt still works if storage is unavailable.
		}
	}, []);

	const cleanup = useMutation({
		mutationFn: async () => {
			const { data, error } = await apiClient.POST("/api/v1/sessions/cleanup", {
				body: { sessionIds: preview.data?.sessions.map((session) => session.sessionId) ?? [] },
			});
			if (error || !data) throw new Error(apiErrorMessage(error, t("legacyCleanup.cleanupFailed")));
			return data;
		},
		onSuccess: (result) => {
			finishPrompt();
			void queryClient.invalidateQueries({ queryKey: ["workspaces"] });
			showGlobalToast(t("legacyCleanup.completed", {
				cleaned: result.cleaned.length,
				skipped: result.skipped.length,
			}));
		},
	});

	const hasEnoughToPrompt = Boolean(
		preview.data?.sessions.length && preview.data.totalBytes >= PROMPT_THRESHOLD_BYTES,
	);
	useEffect(() => {
		if (dismissed || !preview.isSuccess || !preview.data) return;
		if (hasEnoughToPrompt) setOpen(true);
		else if (!preview.data.incomplete) finishPrompt();
	}, [dismissed, finishPrompt, hasEnoughToPrompt, preview.data, preview.isSuccess]);

	if (!preview.data || !hasEnoughToPrompt || dismissed) return null;
	const size = formatBytes(preview.data.totalBytes);
	const description = (
		<div className="space-y-2">
			<p>{t("legacyCleanup.description", { count: preview.data.sessions.length, size })}</p>
			<p>{t("legacyCleanup.estimateNote")}</p>
			<p>{t("legacyCleanup.ignoredWarning")}</p>
			{preview.data.incomplete ? <p>{t("legacyCleanup.incomplete")}</p> : null}
		</div>
	);

	return (
		<ConfirmDialog
			busy={cleanup.isPending}
			cancelLabel={t("legacyCleanup.keep")}
			confirmAriaLabel={t("legacyCleanup.confirmAria")}
			confirmLabel={t("legacyCleanup.confirm")}
			destructive
			description={description}
			error={cleanup.error ? apiErrorMessage(cleanup.error, t("legacyCleanup.cleanupFailed")) : null}
			onConfirm={() => cleanup.mutate()}
			onOpenChange={(nextOpen) => {
				if (!nextOpen) {
					if (!cleanup.isPending) finishPrompt();
					return;
				}
				setOpen(true);
			}}
			open={open}
			title={t("legacyCleanup.title", { size })}
		/>
	);
}
