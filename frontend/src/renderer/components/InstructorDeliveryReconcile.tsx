import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useInstructorReconcile, type InstructorWork } from "../hooks/useInstructor";
import { Button } from "./ui/button";

export function InstructorDeliveryReconcile({ work, snapshotId, onSaved }: { work: InstructorWork; snapshotId: string; onSaved: () => Promise<void> }) {
	const { t } = useTranslation();
	const reconcile = useInstructorReconcile();
	const [observation, setObservation] = useState("");
	const [note, setNote] = useState("");
	const [error, setError] = useState<string | null>(null);
	useEffect(() => { setObservation(""); setNote(""); setError(null); }, [work.id, snapshotId, work.gate?.dispatchId]);
	if (work.gate?.kind !== "delivery_uncertain" || !work.gate.dispatchId) return null;
	async function submit(event: FormEvent) {
		event.preventDefault();
		if (!snapshotId || !work.gate?.dispatchId || !observation) return;
		setError(null);
		try {
			await reconcile.mutateAsync({ snapshotId, workId: work.id, dispatchId: work.gate.dispatchId, accepted: observation === "received", note: note.trim() });
			await onSaved();
		} catch (cause) { setError(cause instanceof Error ? cause.message : t("instructor.commandFailed")); }
	}
	return <form className="mt-5 space-y-3 rounded-lg border border-border p-4" onSubmit={(event) => void submit(event)}><h3 className="font-medium">{t("instructor.reconcile")}</h3><p className="text-xs text-muted-foreground">{t("instructor.reconcileHint")}</p><p className="break-all font-mono text-xs">{work.gate.dispatchId} · {work.gate.targetSessionId || t("instructor.unknown")}</p>{work.suspendedGate ? <p className="text-sm">{t("instructor.suspendedGate", { title: work.suspendedGate.title })}</p> : null}<label className="block text-sm" htmlFor="instructor-delivery-observation">{t("instructor.deliveryObservation")}</label><select id="instructor-delivery-observation" required value={observation} onChange={(event) => setObservation(event.target.value)} className="w-full rounded-md border border-input bg-background p-2 text-sm"><option value="">{t("instructor.chooseObservation")}</option><option value="received">{t("instructor.observedReceived")}</option><option value="absent">{t("instructor.observedAbsent")}</option></select><label className="block text-sm" htmlFor="instructor-delivery-note">{t("instructor.observationNote")}</label><textarea id="instructor-delivery-note" required maxLength={2048} value={note} onChange={(event) => setNote(event.target.value)} className="min-h-20 w-full rounded-md border border-input bg-background p-2 text-sm" />{error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}<Button type="submit" disabled={reconcile.isPending || !snapshotId || !observation || !note.trim()}>{t("instructor.recordObservation")}</Button></form>;
}
