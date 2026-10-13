import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useInstructorRecoveryPreview, useRecoverInstructorSession, type InstructorRecoveryStatus } from "../hooks/useInstructorRecovery";
import { useUiStore } from "../stores/ui-store";
import type { InstructorWork } from "../hooks/useInstructor";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./ui/dialog";

function isConfigurationBlock(code: string | null | undefined) {
	return Boolean(code && (/policy|config/i.test(code) || ["INSTRUCTOR_RESUME_BLOCKED", "UNSUPPORTED_EFFORT", "UNSUPPORTED_MODEL"].includes(code)));
}

export function InstructorRecoveryDialog({ work, onClose, onRecovered }: { work: InstructorWork; onClose: () => void; onRecovered: () => Promise<void> }) {
	const { t } = useTranslation();
	const openSettings = useUiStore((state) => state.openProjectSettings);
	const preview = useInstructorRecoveryPreview();
	const recover = useRecoverInstructorSession();
	const [capture, setCapture] = useState<InstructorRecoveryStatus | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [errorCode, setErrorCode] = useState<string | null>(null);
	const inFlight = useRef(false);
	const mounted = useRef(true);
	const busy = preview.isPending || recover.isPending;
	const configurationBlocked = !capture?.ready && (isConfigurationBlock(capture?.reasonCode) || isConfigurationBlock(errorCode));
	// GET may retain a historical failure after settings change. A fresh explicit
	// inspection releases the local retry guard; the daemon admits the retry.
	const retryNeedsInspection = isConfigurationBlock(errorCode);
	function reportError(cause: unknown) {
		setError(cause instanceof Error ? cause.message : t("instructor.commandFailed"));
		const code = (cause as { code?: unknown } | null)?.code;
		setErrorCode(typeof code === "string" ? code : null);
	}
	async function inspect() {
		if (inFlight.current) return;
		inFlight.current = true; setError(null); setErrorCode(null); setCapture(null);
		try { const result = await preview.mutateAsync(work.id); if (mounted.current) setCapture(result); }
		catch (cause) { if (mounted.current) reportError(cause); }
		finally { inFlight.current = false; }
	}
	useEffect(() => { mounted.current = true; void inspect(); return () => { mounted.current = false; }; }, []);
	async function restore() {
		if (inFlight.current || retryNeedsInspection || !capture || capture.ready || capture.action === "none" || !capture.sessionId || !capture.sessionCreatedAt || !capture.providerConversationId) return;
		inFlight.current = true; setError(null); setErrorCode(null);
		try {
			const { workId, workRevision, policyVersion, sessionId, sessionCreatedAt, providerConversationId, controllerGeneration } = capture;
			const result = await recover.mutateAsync({ workId, workRevision, policyVersion, sessionId, sessionCreatedAt, providerConversationId, controllerGeneration: controllerGeneration ?? "" });
			setCapture(result); await onRecovered();
		} catch (cause) { reportError(cause); }
		finally { inFlight.current = false; }
	}
	return <Dialog open onOpenChange={(open) => { if (!open && !busy) onClose(); }}><DialogContent className="instructor-board-dialog max-h-[85vh] max-w-2xl overflow-y-auto">
		<DialogTitle>{t("instructor.recovery.title")}</DialogTitle><DialogDescription>{t("instructor.recovery.hint")}</DialogDescription>
		<p className="text-sm font-medium">{work.name}</p>
		{preview.isPending ? <p>{t("instructor.loading")}</p> : null}
		{capture ? <section className="space-y-3">
			<p role={capture.ready ? "status" : undefined} className="text-sm">{capture.ready ? t("instructor.recovery.ready") : capture.reason}</p>
			{capture.sessionId ? <p className="break-all text-xs text-muted-foreground">{t("instructor.recovery.session")}: {capture.sessionId}</p> : null}
			<p className="text-xs text-muted-foreground">{t("instructor.recovery.gatesPreserved")}</p>
			{!capture.ready && capture.action !== "none" ? <Button disabled={busy || retryNeedsInspection || !capture.sessionId || !capture.sessionCreatedAt || !capture.providerConversationId} onClick={() => void restore()}>{t("instructor.recovery.reconnect")}</Button> : null}
			<details className="text-xs"><summary>{t("instructor.recovery.diagnostics")}</summary><p className="mt-2 break-all">{capture.reasonCode} · {t("instructor.board.revision", { revision: capture.workRevision })} · {capture.policyVersion}</p>{capture.providerConversationId ? <p className="mt-2 break-all">{capture.providerConversationId}</p> : null}</details>
		</section> : null}
		{error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
		{configurationBlocked ? <Button variant="outline" disabled={busy} onClick={() => openSettings(work.projectId, "agents")}>{t("instructor.recovery.openSettings")}</Button> : null}
		<div className="flex flex-wrap gap-2"><Button variant="outline" disabled={busy} onClick={() => void inspect()}>{t("instructor.recovery.recheck")}</Button><Button variant="ghost" disabled={busy} onClick={onClose}>{t("instructor.recovery.close")}</Button></div>
	</DialogContent></Dialog>;
}
