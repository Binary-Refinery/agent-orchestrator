import { useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import type { InstructorPolicy, InstructorWork } from "../hooks/useInstructor";
import { useApplyInstructorPolicyRebind, useInstructorPolicyRebindPreview, type InstructorPolicyRebindInput, type InstructorPolicyRebindPreview } from "../hooks/useInstructorRecovery";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./ui/dialog";

function RoleSelection({ policy, role }: { policy: InstructorPolicy; role: string }) {
	const settings = policy.roles?.[role];
	if (!settings) return <span>—</span>;
	return <span>{[settings.harness, settings.model, settings.effortByModel?.[settings.model] || settings.effort].filter(Boolean).join(" · ")}</span>;
}

// Mounted with a captured Work and snapshot. Neither board polling nor project
// configuration refreshes are allowed to rewrite the proposal being signed.
export function InstructorPolicyRebindDialog({ work, snapshotId, onClose, onApplied }: { work: InstructorWork; snapshotId: string; onClose: () => void; onApplied: () => Promise<void> }) {
	const { t } = useTranslation();
	const inspect = useInstructorPolicyRebindPreview();
	const apply = useApplyInstructorPolicyRebind();
	const [reason, setReason] = useState("");
	const [approval, setApproval] = useState("");
	const [proposal, setProposal] = useState<{ input: InstructorPolicyRebindInput; preview: InstructorPolicyRebindPreview } | null>(null);
	const [error, setError] = useState<string | null>(null);
	const inFlight = useRef(false);
	const busy = inspect.isPending || apply.isPending;
	async function preview(event: FormEvent) {
		event.preventDefault();
		if (inFlight.current || !reason.trim()) return;
		inFlight.current = true; setError(null); setProposal(null); setApproval("");
		try {
			const input = { workId: work.id, snapshotId, reason: reason.trim() };
			const result = await inspect.mutateAsync(input);
			setProposal({ input: { ...input, targetPolicyVersion: result.targetPolicyVersion }, preview: result });
		} catch (cause) { setError(cause instanceof Error ? cause.message : t("instructor.commandFailed")); }
		finally { inFlight.current = false; }
	}
	async function grant() {
		if (inFlight.current || !proposal || approval !== proposal.preview.exactApproval) return;
		inFlight.current = true; setError(null);
		try { await apply.mutateAsync({ ...proposal.input, approval }); await onApplied(); onClose(); }
		catch (cause) { setError(cause instanceof Error ? cause.message : t("instructor.commandFailed")); }
		finally { inFlight.current = false; }
	}
	const previewResult = proposal?.preview;
	const roles = [...new Set([...Object.keys(previewResult?.beforePolicy.roles ?? {}), ...Object.keys(previewResult?.effectivePolicy.roles ?? {})])];
	return <Dialog open onOpenChange={(open) => { if (!open && !busy) onClose(); }}><DialogContent className="instructor-board-dialog max-h-[85vh] max-w-3xl overflow-y-auto">
		<DialogTitle>{t("instructor.rebind.title")}</DialogTitle><DialogDescription>{t("instructor.rebind.hint")}</DialogDescription>
		<p className="break-all text-xs text-muted-foreground">{work.name} · {work.id} · {t("instructor.board.revision", { revision: work.revision })} · {snapshotId}</p>
		<form className="space-y-3" onSubmit={(event) => void preview(event)}>
			<label className="block text-sm" htmlFor="instructor-rebind-reason">{t("instructor.rebind.reason")}</label>
			<textarea id="instructor-rebind-reason" value={reason} required maxLength={2048} disabled={busy} className="min-h-20 w-full rounded-md border border-input bg-background p-2 text-sm" onChange={(event) => { setReason(event.target.value); setProposal(null); setApproval(""); setError(null); }} />
			<Button type="submit" variant="outline" disabled={busy || !reason.trim()}>{t("instructor.rebind.preview")}</Button>
		</form>
		{previewResult ? <section className="space-y-3 rounded-md border border-border p-3">
			<h3 className="font-medium">{t("instructor.rebind.proposal")}</h3>
			<p className="break-all text-xs">{previewResult.beforePolicyVersion} → {previewResult.targetPolicyVersion}</p>
			<p className="text-xs text-muted-foreground">{t("instructor.configuration.source")}: {previewResult.policySource}</p>
			<div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead><tr><th>{t("instructor.role")}</th><th>{t("instructor.rebind.before")}</th><th>{t("instructor.rebind.after")}</th></tr></thead><tbody>{roles.map((role) => <tr key={role}><th className="p-2 align-top">{role}</th><td className="p-2 align-top"><RoleSelection policy={previewResult.beforePolicy} role={role} /></td><td className="p-2 align-top"><RoleSelection policy={previewResult.effectivePolicy} role={role} /></td></tr>)}</tbody></table></div>
			<details className="text-xs"><summary>{t("instructor.rebind.fullComparison")}</summary><div className="mt-2 grid gap-3"><h4>{t("instructor.rebind.before")}</h4><pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all">{JSON.stringify(previewResult.beforePolicy, null, 2)}</pre><h4>{t("instructor.rebind.after")}</h4><pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all">{JSON.stringify(previewResult.effectivePolicy, null, 2)}</pre></div></details>
			<p className="text-sm">{t("instructor.rebind.history", { count: previewResult.historicalEvidenceCount })}</p>
			<p className="text-sm">{t("instructor.rebind.invalidated")}: {(previewResult.invalidatedTransitions ?? []).join(", ") || "—"}</p>
			<p className="text-sm">{t("instructor.rebind.exceptions")}: {(previewResult.supersededExceptionIds ?? []).join(", ") || "—"}</p>
			<p className="text-sm">{t("instructor.rebind.noExecution")}</p>
			<p className="break-all rounded bg-muted p-2 font-mono text-xs">{previewResult.exactApproval}</p>
			<label className="block text-sm" htmlFor="instructor-rebind-approval">{t("instructor.rebind.approval")}</label><Input id="instructor-rebind-approval" value={approval} autoComplete="off" disabled={busy} onChange={(event) => setApproval(event.target.value)} />
			<Button disabled={busy || approval !== previewResult.exactApproval} onClick={() => void grant()}>{t("instructor.rebind.apply")}</Button>
		</section> : null}
		{error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
	</DialogContent></Dialog>;
}
