import { useEffect, useRef, useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { agentModelsQueryOptions } from "../hooks/useAgentModelsQuery";
import { useAgentReadinessQuery } from "../hooks/useAgentReadinessQuery";
import type { components } from "../../api/schema";
import { useApplyInstructorException, useInstructorExceptionPreview, type InstructorExceptionInput, type InstructorExceptionPreview, type InstructorWork } from "../hooks/useInstructor";
import { AgentModelCombobox } from "./settings/AgentModelCombobox";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./ui/dialog";

function localExpiry() {
	const expiry = new Date(Date.now() + 4 * 60 * 60 * 1000);
	return new Date(expiry.getTime() - expiry.getTimezoneOffset() * 60 * 1000).toISOString().slice(0, 16);
}

type ExecutionBudgets = NonNullable<InstructorExceptionInput["executionBudgets"]>;
type BudgetUsage = components["schemas"]["InstructorBudgetUsage"];
const budgetFields = [
	["maxConcurrentSessions", "concurrentSessions", "instructor.budgets.concurrent"],
	["maxSessions", "sessions", "instructor.budgets.sessions"],
	["maxTurns", "turns", "instructor.budgets.turns"],
	["maxDelegations", "delegations", "instructor.budgets.delegations"],
] as const;
function workBudgets(work: InstructorWork): ExecutionBudgets {
	return work.policy.budgets ?? { maxConcurrentSessions: 0, maxSessions: 0, maxTurns: 0, maxDelegations: 0 };
}

export function InstructorExceptionDialog({ open, work: liveWork, snapshotId: liveSnapshotId, observedWork, budgetUsage, clickApproval = false, onClose, onApplied }: { open: boolean; work: InstructorWork; snapshotId: string; observedWork?: Pick<InstructorWork, "revision" | "policyVersion">; budgetUsage?: BudgetUsage; clickApproval?: boolean; onClose: () => void; onApplied: () => Promise<void> }) {
	const { t } = useTranslation();
	const [capture, setCapture] = useState(() => ({ work: structuredClone(liveWork), snapshotId: liveSnapshotId }));
	const { work, snapshotId } = capture;
	const observation = observedWork ?? liveWork;
	const facts = JSON.stringify([liveWork.id, observation.revision, observation.policyVersion, ...budgetFields.map(([, usage]) => budgetUsage?.[usage] ?? null)]);
	const factsRef = useRef(facts);
	factsRef.current = facts;
	const previousFacts = useRef<{ workId: string; value: string } | null>(null);
	const captureGeneration = useRef(0);
	const scopeCurrent = work.id === liveWork.id && work.revision === observation.revision && work.policyVersion === observation.policyVersion;
	const previewMutation = useInstructorExceptionPreview();
	const apply = useApplyInstructorException();
	const agents = useAgentReadinessQuery().data?.agents ?? [];
	const [changeRole, setChangeRole] = useState(false);
	const [role, setRole] = useState("worker");
	const [harness, setHarness] = useState("");
	const [model, setModel] = useState("");
	const [effort, setEffort] = useState("");
	const [maxFixRounds, setMaxFixRounds] = useState("");
	const [changeBudgets, setChangeBudgets] = useState(false);
	const [executionBudgets, setExecutionBudgets] = useState<ExecutionBudgets>(() => workBudgets(work));
	const [reason, setReason] = useState("");
	const [expiresAt, setExpiresAt] = useState(localExpiry);
	const [approval, setApproval] = useState("");
	const [proposal, setProposal] = useState<InstructorExceptionInput | null>(null);
	const [preview, setPreview] = useState<InstructorExceptionPreview | null>(null);
	const [previewFacts, setPreviewFacts] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);
	const catalog = useQuery(agentModelsQueryOptions(open && changeRole ? harness : "", work.projectId));
	const busy = previewMutation.isPending || apply.isPending;
	const nativeValid = Boolean(harness && model && effort && catalog.data?.models.some((item) => item.id === model && item.efforts?.includes(effort)));
	const budgetsValid = budgetFields.every(([limit]) => Number.isInteger(executionBudgets[limit]) && executionBudgets[limit] >= 0 && executionBudgets[limit] <= 1_000_000);

	function clearPreview() { setPreview(null); setPreviewFacts(null); setProposal(null); setApproval(""); setError(null); }
	function chooseRole(value: string) {
		const settings = work.policy.roles?.[value];
		setRole(value); setHarness(settings?.harness ?? ""); setModel(settings?.model ?? "");
		setEffort(settings?.effortByModel?.[settings.model] ?? (settings?.effort !== "maximum" && settings?.effort !== "maximum-native" ? settings?.effort ?? "" : ""));
		clearPreview();
	}
	useEffect(() => {
		captureGeneration.current++;
		if (open) setCapture({ work: structuredClone(liveWork), snapshotId: liveSnapshotId });
	}, [open, liveWork.id]);
	useEffect(() => {
		if (!open) { previousFacts.current = null; return; }
		const previous = previousFacts.current;
		previousFacts.current = { workId: liveWork.id, value: facts };
		if (!previous || previous.workId !== liveWork.id || previous.value === facts) return;
		clearPreview();
		if (preview || proposal) setError(t("instructor.exceptionObservationChanged"));
	}, [open, liveWork.id, facts]);
	useEffect(() => {
		if (!open) return;
		setChangeRole(false); chooseRole("worker"); setMaxFixRounds(""); setChangeBudgets(false); setExecutionBudgets(workBudgets(work)); setReason(""); setExpiresAt(localExpiry());
	}, [open, work.id]);

	async function inspect(event: FormEvent) {
		event.preventDefault();
		if (!scopeCurrent) return;
		const inspectedFacts = facts;
		const inspectedGeneration = captureGeneration.current;
		setError(null);
		try {
			const input: InstructorExceptionInput = { snapshotId, workId: work.id, reason: reason.trim(), expiresAt: new Date(expiresAt).toISOString(), ...(changeRole ? { role, selection: { harness, model, effort } } : {}), ...(maxFixRounds ? { maxFixRounds: Number(maxFixRounds) } : {}), ...(changeBudgets ? { executionBudgets: { ...executionBudgets } } : {}) };
			const result = await previewMutation.mutateAsync(input);
			if (captureGeneration.current !== inspectedGeneration) return;
			if (factsRef.current !== inspectedFacts) { setError(t("instructor.exceptionObservationChanged")); return; }
			setProposal(input); setPreview(result); setPreviewFacts(inspectedFacts); setApproval("");
		} catch (cause) { setError(cause instanceof Error ? cause.message : t("instructor.commandFailed")); }
	}
	async function grant() {
		if (!scopeCurrent || !proposal || !preview || previewFacts !== facts || approval !== preview.exactApproval) return;
		setError(null);
		try { await apply.mutateAsync({ ...proposal, approval }); await onApplied(); onClose(); }
		catch (cause) { setError(cause instanceof Error ? cause.message : t("instructor.commandFailed")); }
	}
	const effectiveRole = proposal?.role ? preview?.effectivePolicy.roles?.[proposal.role] : undefined;
	return <Dialog open={open} onOpenChange={(next) => { if (!next && !busy) onClose(); }}><DialogContent className="instructor-board-dialog max-h-[85vh] max-w-2xl overflow-y-auto"><DialogTitle>{t("instructor.exception")}</DialogTitle><DialogDescription>{t("instructor.exceptionHint")}</DialogDescription><p className="break-all text-xs text-muted-foreground">{work.name} · {work.id} · {snapshotId}</p><form className="space-y-4" onSubmit={(event) => void inspect(event)}><fieldset disabled={busy} className="space-y-4"><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={changeRole} onChange={(event) => { setChangeRole(event.target.checked); clearPreview(); }} />{t("instructor.exceptionRole")}</label>{changeRole ? <div className="space-y-3 rounded-md border border-border p-3"><select aria-label={t("instructor.role")} value={role} onChange={(event) => chooseRole(event.target.value)} className="rounded-md border border-input bg-background p-2 text-sm"><option value="worker">{t("settings.models.workerRole")}</option><option value="reviewer">{t("settings.models.reviewerRole")}</option></select><select aria-label={t("instructor.harness")} value={harness} onChange={(event) => { setHarness(event.target.value); setModel(""); setEffort(""); clearPreview(); }} className="ml-2 rounded-md border border-input bg-background p-2 text-sm"><option value="">{t("instructor.unknown")}</option>{harness && !agents.some((item) => item.id === harness) ? <option value={harness}>{harness}</option> : null}{agents.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}</select><AgentModelCombobox aria-label={t("instructor.requested")} value={model} models={catalog.data?.models ?? []} allowCustom={false} onChange={(value) => { setModel(value); setEffort(work.policy.roles?.[role]?.harness === harness ? work.policy.roles?.[role]?.effortByModel?.[value] ?? "" : ""); clearPreview(); }} onCustom={(value) => { setModel(value); setEffort(""); clearPreview(); }} pinSelection showFollowAgentAction={false} disabled={busy || !harness} tuning={{ effort, requireExplicitEffort: true, onEffortChange: (value) => { setEffort(value); clearPreview(); } }} />{catalog.error ? <p role="alert" className="text-xs text-destructive">{catalog.error.message}</p> : null}{!nativeValid ? <p className="text-xs text-muted-foreground">{t("instructor.missingMapping")}</p> : null}</div> : null}<section className="space-y-3 rounded-md border border-border p-3"><h3 className="font-medium">{t("instructor.budgets.title")}</h3><p className="text-xs text-muted-foreground">{t("instructor.budgets.hint")}</p><dl className="grid gap-2 text-sm sm:grid-cols-2">{budgetFields.map(([limit, usage, label]) => <div key={limit}><dt className="text-muted-foreground">{t(label)}</dt><dd>{budgetUsage ? budgetUsage[usage] : t("instructor.unknown")} / {work.policy.budgets && work.policy.budgets[limit] ? work.policy.budgets[limit] : t("instructor.budgets.unlimited")}</dd></div>)}</dl><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={changeBudgets} onChange={(event) => { setChangeBudgets(event.target.checked); clearPreview(); }} />{t("instructor.budgets.adjust")}</label>{changeBudgets ? <div className="grid gap-3 sm:grid-cols-2">{budgetFields.map(([limit, , label]) => <label key={limit} className="block text-sm">{t(label)}<Input aria-label={t(label)} type="number" required min={0} max={1_000_000} value={executionBudgets[limit]} onChange={(event) => { setExecutionBudgets({ ...executionBudgets, [limit]: Number(event.target.value) }); clearPreview(); }} /></label>)}</div> : null}</section><label className="block text-sm" htmlFor="instructor-exception-rounds">{t("instructor.exceptionRounds", { current: work.policy.maxFixRounds })}</label><Input id="instructor-exception-rounds" type="number" min={work.policy.maxFixRounds + 1} max={10} value={maxFixRounds} onChange={(event) => { setMaxFixRounds(event.target.value); clearPreview(); }} /><label className="block text-sm" htmlFor="instructor-exception-reason">{t("instructor.exceptionReason")}</label><textarea id="instructor-exception-reason" required maxLength={2048} value={reason} onChange={(event) => { setReason(event.target.value); clearPreview(); }} className="min-h-20 w-full rounded-md border border-input bg-background p-2 text-sm" /><label className="block text-sm" htmlFor="instructor-exception-expiry">{t("instructor.exceptionExpiry")}</label><Input id="instructor-exception-expiry" type="datetime-local" required value={expiresAt} onChange={(event) => { setExpiresAt(event.target.value); clearPreview(); }} /><Button type="submit" variant="outline" disabled={busy || !scopeCurrent || !snapshotId || !reason.trim() || (!changeRole && !maxFixRounds && !changeBudgets) || (changeRole && !nativeValid) || (changeBudgets && !budgetsValid)}>{t("instructor.exceptionPreview")}</Button></fieldset></form>{preview && proposal && previewFacts === facts ? <section className="space-y-3 rounded-md border border-border p-4"><h3 className="font-medium">{t("instructor.exceptionProposed")}</h3>{effectiveRole ? <p className="text-sm">{proposal.role} · {effectiveRole.harness} · {effectiveRole.model} · {effectiveRole.effortByModel?.[effectiveRole.model] || effectiveRole.effort}</p> : null}<p className="text-sm">{t("instructor.exceptionEffectiveRounds", { rounds: preview.effectivePolicy.maxFixRounds })}</p>{proposal.executionBudgets ? <dl className="grid gap-2 text-sm sm:grid-cols-2">{budgetFields.map(([limit, , label]) => <div key={limit}><dt className="text-muted-foreground">{t(label)}</dt><dd>{preview.effectivePolicy.budgets?.[limit] || t("instructor.budgets.unlimited")}</dd></div>)}</dl> : null}<p className="text-sm">{proposal.reason}</p><p className="text-xs text-muted-foreground">{proposal.expiresAt}</p><p className="break-all rounded bg-muted p-2 font-mono text-xs">{preview.exactApproval}</p>{clickApproval ? <label className="flex items-start gap-2 text-sm"><input type="checkbox" disabled={busy} checked={approval === preview.exactApproval} onChange={(event) => setApproval(event.target.checked ? preview.exactApproval : "")} />{t("instructor.budgets.signProposal")}</label> : <><label className="block text-sm" htmlFor="instructor-exception-approval">{t("instructor.exceptionApproval")}</label><Input id="instructor-exception-approval" value={approval} onChange={(event) => setApproval(event.target.value)} autoComplete="off" disabled={busy} /></>}<Button type="button" disabled={busy || !scopeCurrent || previewFacts !== facts || approval !== preview.exactApproval} onClick={() => void grant()}>{t("instructor.exceptionGrant")}</Button></section> : null}{!scopeCurrent ? <p role="alert" className="text-sm text-destructive">{t("instructor.exceptionScopeChanged")}</p> : null}{error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}</DialogContent></Dialog>;
}
