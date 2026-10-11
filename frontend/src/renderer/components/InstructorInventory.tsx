import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import type { components } from "../../api/schema";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./ui/dialog";

type Inventory = components["schemas"]["InstructorInventoryResponse"];
type Resource = components["schemas"]["InstructorGitResourceInventory"];

function displayTime(value?: string) {
	if (!value || value.startsWith("0001-")) return "—";
	const date = new Date(value);
	return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

export function InstructorInventory({ data, loading, error, onReconcile, reconciling = false, reconcileError }: {
	data?: Inventory;
	loading: boolean;
	error: Error | null;
	onReconcile?: (resourceId: string, lastSeenAt: string) => Promise<void>;
	reconciling?: boolean;
	reconcileError?: Error | null;
}) {
	const { t } = useTranslation();
	const navigate = useNavigate();
	const [selection, setSelection] = useState<Resource | null>(null);
	const [submitting, setSubmitting] = useState(false);
	const [submissionError, setSubmissionError] = useState<string | null>(null);
	const unassigned = data?.sessions.filter((session) => session.assignment === "unassigned") ?? [];
	const resources = data?.resources ?? [];
	const busy = reconciling || submitting;

	async function reconcile() {
		if (!selection || !onReconcile || !selection.lastSeenAt || busy) return;
		setSubmitting(true);
		setSubmissionError(null);
		try {
			// Keep the exact captured ID and timestamp across background refetches.
			await onReconcile(selection.id, selection.lastSeenAt);
			setSelection(null);
		} catch (cause) {
			setSubmissionError(cause instanceof Error ? cause.message : t("instructor.commandFailed"));
		} finally {
			setSubmitting(false);
		}
	}

	return <section className="rounded-lg border border-border p-4">
		<h2 className="font-medium">{t("instructor.resources")}</h2>
		<p className="mt-1 text-xs text-muted-foreground">{t("instructor.inventoryHint")}</p>
		{loading ? <p className="mt-2 text-sm">{t("instructor.loading")}</p> : null}
		{error ? <p role="alert" className="mt-2 text-sm text-destructive">{error.message}</p> : null}
		{data ? <>
			{!data.enabled ? <p className="mt-2 text-sm text-muted-foreground">{t("instructor.inventoryDisabled")}</p> : null}
			<p className="mt-2 text-xs text-muted-foreground">{data.cached ? t("instructor.inventoryCached", { time: displayTime(data.capturedAt) }) : t("instructor.inventoryPending")}</p>
			{data.nextScanAt ? <p className="mt-1 text-xs text-muted-foreground">{t("instructor.inventoryNextScan", { time: displayTime(data.nextScanAt) })}</p> : null}
			{data.scanError ? <p role="alert" className="mt-2 text-sm text-destructive">{t("instructor.inventoryScanError", { message: data.scanError })}</p> : null}
			{unassigned.length ? <ul className="mt-3 space-y-2">{unassigned.map((session) => <li key={session.id} className="flex flex-wrap items-center justify-between gap-2 rounded bg-muted/30 p-3 text-sm">
				<div><p>{session.displayName || session.id} · {session.projectId}</p><p className="mt-1 text-xs text-muted-foreground">{session.kind} · {session.harness} · {session.activityState}</p><p className="mt-1 break-all text-xs text-muted-foreground">{session.branch || session.workspacePath || "—"}</p></div>
				<Button size="sm" variant="outline" onClick={() => void navigate({ to: "/sessions/$sessionId", params: { sessionId: session.id } })}>{t("instructor.openSession")}</Button>
			</li>)}</ul> : <p className="mt-3 text-sm text-muted-foreground">{t("instructor.noUnassigned")}</p>}
			<h3 className="mt-4 text-sm font-medium">{t("instructor.gitResources")}</h3>
			{resources.length ? <ul className="mt-2 space-y-2">{resources.map((resource) => <li key={resource.id} className="rounded bg-muted/30 p-3 text-xs">
				<p className="font-medium">{resource.kind} · {resource.projectId} · {resource.assignment}</p>
				<p className="mt-1 break-all text-muted-foreground">{resource.path || resource.repositoryPath}</p>
				{resource.branch ? <p className="mt-1 break-all">{resource.branch} · {resource.sha || "—"}</p> : null}
				<p className="mt-1 text-muted-foreground">{resource.status}{resource.dirty ? ` · ${t("instructor.uncommitted")}` : ""}</p>
				{resource.workIds?.length ? <p className="mt-1 break-all">{resource.workIds.join(" · ")}</p> : null}
				{resource.sessionIds?.length ? <p className="mt-1 break-all text-muted-foreground">{resource.sessionIds.join(" · ")}</p> : null}
				<p className="mt-1 text-muted-foreground">{t("instructor.inventoryFirstSeen", { time: displayTime(resource.firstSeenAt) })} · {t("instructor.inventoryLastSeen", { time: displayTime(resource.lastSeenAt) })}</p>
				{resource.notObservedSince ? <p className="mt-1 text-muted-foreground">{t("instructor.inventoryNotObservedSince", { time: displayTime(resource.notObservedSince) })}</p> : null}
				{resource.reconciledAt ? <p className="mt-1 text-muted-foreground">{t("instructor.inventoryReconciledAt", { time: displayTime(resource.reconciledAt) })}</p> : null}
				{resource.error ? <p role="alert" className="mt-1 text-destructive">{resource.error}</p> : null}
				{resource.status === "not-observed" && !resource.reconciledAt && resource.lastSeenAt && !resource.lastSeenAt.startsWith("0001-") ? onReconcile && data.enabled ? <Button className="mt-2" size="sm" variant="outline" disabled={busy} onClick={() => { setSelection({ ...resource }); setSubmissionError(null); }}>{t("instructor.inventoryReconcile")}</Button> : <p className="mt-2 text-muted-foreground">{t("instructor.inventoryReconcileUnavailable")}</p> : null}
			</li>)}</ul> : <p className="mt-2 text-sm text-muted-foreground">{t("instructor.noAttentionGit")}</p>}
		</> : null}
		<Dialog open={Boolean(selection)} onOpenChange={(open) => { if (!open && !busy) setSelection(null); }}><DialogContent>
			<DialogTitle>{t("instructor.inventoryReconcileTitle")}</DialogTitle>
			<DialogDescription>{t("instructor.inventoryReconcileExplanation")}</DialogDescription>
			{selection ? <><p className="break-all text-sm">{selection.branch || selection.path || selection.repositoryPath} · {selection.id}</p><p className="text-xs text-muted-foreground">{t("instructor.inventoryLastSeen", { time: displayTime(selection.lastSeenAt) })}</p></> : null}
			{submissionError || reconcileError ? <p role="alert" className="text-sm text-destructive">{submissionError || reconcileError?.message}</p> : null}
			<div className="flex justify-end gap-2"><Button variant="outline" disabled={busy} onClick={() => setSelection(null)}>{t("instructor.inventoryReconcileCancel")}</Button><Button disabled={busy || !onReconcile} onClick={() => void reconcile()}>{t("instructor.inventoryReconcileConfirm")}</Button></div>
		</DialogContent></Dialog>
	</section>;
}
