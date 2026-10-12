import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Compass, Plus, RefreshCw } from "lucide-react";
import {
	useInstructorCapabilities,
	useInstructorCommand,
	useInstructorDetails,
	useInstructorInventory,
	useInstructorReconcile,
	useInstructorInventoryReconcile,
	useInstructorSnapshot,
	type InstructorCommandInput,
	type InstructorCommandSpec,
	type InstructorSnapshot,
	type InstructorWork,
} from "../hooks/useInstructor";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./ui/dialog";
import type { MessageKey } from "../i18n/messages";
import { InstructorPolicyDialog } from "./InstructorPolicyDialog";
import { InstructorExceptionDialog } from "./InstructorExceptionDialog";
import { InstructorInventory } from "./InstructorInventory";
import { InstructorPlanning } from "./InstructorPlanning";

type WorkSection = "active" | "parked" | "history";
type PendingCommand = {
	spec: InstructorCommandSpec;
	snapshotId: string;
	works: InstructorWork[];
	idempotencyKey: string;
};

function displayTime(value?: string) {
	if (!value || value.startsWith("0001-")) return "—";
	const date = new Date(value);
	return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

// Section placement is presentation only. Every transition and allowed action
// comes from the daemon; the view never infers an approval from a phase name.
function sectionOf(work: InstructorWork): WorkSection {
	if (work.state === "done" || work.state === "cancelled") return "history";
	return work.state === "parked" ? "parked" : "active";
}

const stateLabels: Record<string, MessageKey> = {
	running: "instructor.state.running",
	ready: "instructor.state.ready",
	waiting_human: "instructor.state.waiting_human",
	waiting_external: "instructor.state.waiting_external",
	blocked: "instructor.state.blocked",
	parked: "instructor.state.parked",
	done: "instructor.state.done",
	cancelled: "instructor.state.cancelled",
};

export function InstructorView({ initialWorkId }: { initialWorkId?: string } = {}) {
	const { t } = useTranslation();
	const capabilities = useInstructorCapabilities();
	const enabled = capabilities.data?.enabled === true;
	const query = useInstructorSnapshot(enabled);
	const inventory = useInstructorInventory(enabled);
	const command = useInstructorCommand();
	const inventoryReconcile = useInstructorInventoryReconcile();
	const [snapshot, setSnapshot] = useState<InstructorSnapshot | null>(null);
	const [delta, setDelta] = useState<InstructorSnapshot | null>(null);
	const [section, setSection] = useState<WorkSection>("active");
	const [selected, setSelected] = useState<Set<string>>(new Set());
	const [batchAction, setBatchAction] = useState("");
	const [pending, setPending] = useState<PendingCommand | null>(null);
	const directGoIntent = useRef<{ digest: string; key: string } | null>(null);
	const [approval, setApproval] = useState("");
	const [newOpen, setNewOpen] = useState(false);
	const [newDraft, setNewDraft] = useState<Partial<NewInput> | null>(null);
	const [newSnapshotId, setNewSnapshotId] = useState<string | undefined>();
	const [policyOpen, setPolicyOpen] = useState(false);
	const [detailsId, setDetailsId] = useState<string | null>(initialWorkId ?? null);
	const [actionError, setActionError] = useState<string | null>(null);
	const [reconcileNotice, setReconcileNotice] = useState(false);
	const [exceptionOpen, setExceptionOpen] = useState(false);
	const [exceptionNotice, setExceptionNotice] = useState(false);
	const details = useInstructorDetails(enabled ? detailsId : null);
	const registry = snapshot?.commands ?? capabilities.data?.commands ?? [];
	const works = snapshot?.works ?? [];
	const visible = works.filter((work) => sectionOf(work) === section);
	const chosen = works.filter((work) => selected.has(work.id));
	const actionCommands = registry.filter((spec) => spec.mutates && spec.action !== "neu");
	const grouped = useMemo(() => {
		const groups = new Map<string, InstructorWork[]>();
		for (const work of visible) groups.set(work.projectId, [...(groups.get(work.projectId) ?? []), work]);
		return groups;
	}, [visible]);

	useEffect(() => {
		if (query.data) setSnapshot(query.data);
	}, [query.data]);
	useEffect(() => setDetailsId(initialWorkId ?? null), [initialWorkId]);

	async function send(body: InstructorCommandInput) {
		setActionError(null);
		try {
			const result = await command.mutateAsync({ ...body, idempotencyKey: body.idempotencyKey ?? crypto.randomUUID() });
			if (result.snapshot) {
				if (body.command === "delta") setDelta(result.snapshot);
				else { setSnapshot(result.snapshot); setDelta(null); setSelected(new Set()); }
			}
			return result;
		} catch (error) {
			setActionError(error instanceof Error ? error.message : t("instructor.commandFailed"));
			return null;
		}
	}

	function run(spec: InstructorCommandSpec, targets: InstructorWork[], snapshotId = snapshot?.id) {
		if (!snapshot || !snapshotId) return;
		if (spec.action === "go" && targets.some((work) => work.gate?.exactApproval)) {
			setApproval("");
			setPending(structuredClone({ spec, snapshotId, works: targets, idempotencyKey: crypto.randomUUID() }));
			return;
		}
		const body: InstructorCommandInput = { command: spec.action, snapshotId, workIds: targets.map((work) => work.id) };
		if (spec.action === "go" && targets.length > 1) {
			body.approvals = targets.map((work) => ({ workId: work.id, approval: "" }));
			const digest = JSON.stringify(body);
			if (directGoIntent.current?.digest !== digest) directGoIntent.current = { digest, key: crypto.randomUUID() };
			body.idempotencyKey = directGoIntent.current.key;
		}
		void send(body);
	}

	async function submitPending(event: FormEvent) {
		event.preventDefault();
		if (!pending || command.isPending || !pending.works.some((work) => work.gate?.exactApproval === approval)) return;
		// The typed literal approves only its captured matching group. Never
		// fill other branches' signatures from the displayed gate metadata.
		const intent = pending;
		const targets = intent.works.filter((work) => !work.gate?.exactApproval || work.gate.exactApproval === approval);
		const result = await send({
			command: intent.spec.action, snapshotId: intent.snapshotId,
			idempotencyKey: intent.idempotencyKey,
			workIds: targets.map((work) => work.id),
			...(targets.length > 1 ? { approvals: targets.map((work) => ({ workId: work.id, approval: work.gate?.exactApproval ? approval : "" })) } : { approval }),
		});
		if (!result) return;
		if (result.skipped?.length) { setActionError(result.skipped.join(" · ")); return; }
		const accepted = new Set(targets.map((work) => work.id));
		const remaining = intent.works.filter((work) => !accepted.has(work.id));
		setPending((current) => current?.idempotencyKey === intent.idempotencyKey ? remaining.length ? { ...intent, works: remaining, idempotencyKey: crypto.randomUUID() } : null : current);
		setApproval("");
	}

	function toggle(workId: string) {
		setSelected((current) => {
			const next = new Set(current);
			if (next.has(workId)) next.delete(workId); else next.add(workId);
			return next;
		});
	}

	return (
		<div className="flex min-h-0 flex-1 flex-col overflow-auto bg-background">
			<header className="flex flex-wrap items-center justify-between gap-4 border-b border-border px-8 py-5">
				<div><h1 className="flex items-center gap-2 text-xl font-semibold"><Compass className="size-5" aria-hidden="true" />{t("instructor.title")}</h1><p className="mt-1 text-sm text-muted-foreground">{t("instructor.description")}</p></div>
				{enabled ? <div className="flex flex-wrap gap-2">
					<Button variant="outline" onClick={() => setPolicyOpen(true)}>{t("instructor.configure")}</Button>
					{registry.filter((spec) => spec.action === "status" || spec.action === "delta" || spec.action === "tore").map((spec) => <Button key={spec.action} variant="outline" disabled={command.isPending || (spec.snapshotRequired && !snapshot)} title={spec.description} onClick={() => void send({ command: spec.action, snapshotId: snapshot?.id })}><RefreshCw className="size-4" aria-hidden="true" />{spec.name}</Button>)}
					{registry.flatMap((spec) => (spec.aliases ?? []).filter((alias) => /(?:all|alle)$/i.test(alias)).map((alias) => <Button key={alias} variant="outline" disabled={command.isPending || !snapshot} title={t("instructor.batchAllHint")} onClick={() => void send({ command: alias, snapshotId: snapshot?.id })}>{alias.charAt(0).toUpperCase() + alias.slice(1)}</Button>))}
					{registry.some((spec) => spec.action === "neu") ? <Button disabled={command.isPending} onClick={() => { setNewDraft(null); setNewSnapshotId(snapshot?.id); setNewOpen(true); }}><Plus aria-hidden="true" />{t("instructor.new")}</Button> : null}
				</div> : null}
			</header>
			<main className="flex flex-col gap-5 p-6">
				{capabilities.isLoading || (enabled && query.isLoading) ? <p className="text-sm text-muted-foreground">{t("instructor.loading")}</p> : null}
				{capabilities.error ? <p role="alert" className="text-sm text-destructive">{t("instructor.unavailable")} {capabilities.error.message}</p> : null}
				{capabilities.data && !enabled ? <div className="rounded-lg border border-border p-5"><h2 className="font-medium">{t("instructor.disabled")}</h2><p className="mt-2 text-sm text-muted-foreground">{capabilities.data.reason || t("instructor.enableHint")}</p></div> : null}
				{enabled && query.error ? <p role="alert" className="text-sm text-destructive">{query.error.message}</p> : null}
				{actionError ? <p role="alert" className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">{actionError}</p> : null}
				{reconcileNotice ? <p role="status" className="rounded-md bg-muted p-3 text-sm">{t("instructor.reconcileSaved")}</p> : null}
				{exceptionNotice ? <p role="status" className="rounded-md bg-muted p-3 text-sm">{t("instructor.exceptionSaved")}</p> : null}
				{enabled && snapshot ? <>
					<InstructorPlanning snapshotId={snapshot.id} busy={command.isPending} onProposal={(job, proposal) => {
						if (job.input.snapshotId !== snapshot.id) return;
						const name = proposal.command.toLocaleLowerCase();
						const spec = registry.find((item) => [item.action, item.name, ...(item.aliases ?? [])].some((alias) => alias.toLocaleLowerCase() === name));
						if (spec?.action === "status" || spec?.action === "delta") { void send({ command: spec.action, snapshotId: job.input.snapshotId }); return; }
						const targets = works.filter((work) => proposal.workIds.includes(work.id));
						if (!spec || !targets.length || targets.length !== proposal.workIds.length || targets.some((work) => !(work.actions ?? []).includes(spec.action))) { setActionError(t("instructor.strategyProposalUnavailable")); return; }
						if (spec.action === "details") { setDetailsId(targets[0].id); return; }
						run(spec, targets, job.input.snapshotId);
					}} onIntake={(job, intake) => {
						if (job.input.snapshotId !== snapshot.id) return;
						setNewDraft({ ...intake, sourceRef: `plan:${job.id}:${job.output?.intake.indexOf(intake) ?? -1}` }); setNewSnapshotId(job.input.snapshotId); setNewOpen(true);
					}} />
					<p className="text-xs text-muted-foreground">{t("instructor.snapshot", { id: snapshot.id, time: displayTime(snapshot.createdAt) })}</p>
					{snapshot.commandConflicts?.length ? <div role="alert" className="rounded-md border border-border p-3 text-sm"><p className="font-medium">{t("instructor.commandConflicts")}</p><ul className="mt-2 space-y-1 text-xs text-muted-foreground">{snapshot.commandConflicts.map((conflict, index) => <li key={index}>{conflict}</li>)}</ul></div> : null}
					{snapshot.metrics ? <dl aria-label={t("instructor.metrics")} className="grid grid-cols-2 gap-3 rounded-lg border border-border p-4 sm:grid-cols-4">
						{([
							["closedLast7Days", "instructor.closedLast7Days"],
							["gatesClearedLast24Hours", "instructor.gatesClearedLast24Hours"],
							["stalled", "instructor.stalled"],
							["recordedViolations", "instructor.recordedViolations"],
							["active", "instructor.metricsActive"],
							["gated", "instructor.metricsGated"],
							["parked", "instructor.metricsParked"],
							["verificationFailures", "instructor.verificationFailures"],
						] as const).map(([key, label]) => <div key={key}><dt className="text-xs text-muted-foreground">{t(label)}</dt><dd className="mt-1 font-medium tabular-nums">{snapshot.metrics[key]}</dd></div>)}
					</dl> : null}
					{registry.some((spec) => spec.parameters?.length || spec.authority) ? <details className="rounded-lg border border-border px-4 py-3"><summary className="cursor-pointer text-sm font-medium">{t("instructor.commandReference")}</summary><dl className="mt-3 space-y-3 text-sm">{registry.map((spec) => <div key={spec.action}><dt className="font-medium">{spec.name}</dt><dd className="mt-1 text-muted-foreground">{spec.description}</dd><dd className="mt-1 text-xs text-muted-foreground">{t("instructor.commandAuthority", { authority: spec.authority, scopes: spec.scopes?.join(", ") || "—" })}</dd>{spec.parameters?.length ? <dd className="mt-1 text-xs">{spec.parameters.map((parameter) => `${parameter.name}: ${parameter.type}${parameter.required ? ` (${t("instructor.required")})` : ""}`).join(" · ")}</dd> : null}{spec.preconditions?.length ? <dd className="mt-1 text-xs text-muted-foreground">{t("instructor.commandPreconditions", { preconditions: spec.preconditions.join(", ") })}</dd> : null}</div>)}</dl></details> : null}
					<div className="flex flex-wrap items-center justify-between gap-3">
						<div role="tablist" aria-label={t("instructor.sections")} className="flex gap-1">{(["active", "parked", "history"] as const).map((name) => <Button key={name} role="tab" aria-selected={section === name} variant={section === name ? "secondary" : "ghost"} onClick={() => setSection(name)}>{t(`instructor.${name}`)}<span className="ml-1 tabular-nums">{works.filter((work) => sectionOf(work) === name).length}</span></Button>)}</div>
						<div className="flex flex-wrap items-center gap-2"><span className="text-xs text-muted-foreground">{t("instructor.selected", { count: chosen.length })}</span><select aria-label={t("instructor.command")} value={batchAction} className="rounded-md border border-input bg-background px-2 py-1.5 text-sm" onChange={(event) => setBatchAction(event.target.value)}><option value="">{t("instructor.chooseCommand")}</option>{actionCommands.map((spec) => <option key={spec.action} value={spec.action} disabled={!chosen.length || chosen.some((work) => !(work.actions ?? []).includes(spec.action))}>{spec.name}</option>)}</select><Button variant="outline" size="sm" disabled={command.isPending || !chosen.length || !batchAction || chosen.some((work) => !(work.actions ?? []).includes(batchAction))} onClick={() => { const spec = registry.find((item) => item.action === batchAction); if (spec) run(spec, chosen); }}>{t("instructor.execute")}</Button></div>
					</div>
					{visible.length === 0 ? <p className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">{t("instructor.empty")}</p> : null}
					{[...grouped].map(([projectId, projectWorks]) => <section key={projectId} aria-label={projectId} className="overflow-hidden rounded-lg border border-border"><h2 className="bg-muted/30 px-4 py-3 font-medium">{inventory.data?.projects.find((project) => project.id === projectId)?.name ?? projectId}</h2><div className="overflow-x-auto"><table className="w-full min-w-[1050px] text-left text-sm"><thead className="border-y border-border text-xs text-muted-foreground"><tr><th className="p-3"><span className="sr-only">{t("instructor.select")}</span></th><th className="p-3">{t("instructor.work")}</th><th className="p-3">{t("instructor.current")}</th><th className="p-3">{t("instructor.lastNext")}</th><th className="p-3">{t("instructor.gateWait")}</th><th className="p-3">{t("instructor.freshness")}</th><th className="p-3">{t("instructor.actions")}</th></tr></thead><tbody>{projectWorks.map((work) => <tr key={work.id} className="border-b border-border last:border-0 align-top"><td className="p-3"><input type="checkbox" aria-label={t("instructor.selectWork", { name: work.name })} checked={selected.has(work.id)} onChange={() => toggle(work.id)} /></td><td className="max-w-64 p-3"><p className="font-medium">{work.name}</p><p className="mt-1 text-xs text-muted-foreground">{work.goal}</p><p className="mt-2 break-all font-mono text-[11px] text-muted-foreground">{work.id}</p><p className="mt-1 text-xs text-muted-foreground">{t("instructor.policy", { version: work.policyVersion })}</p></td><td className="p-3"><p>{work.phase}</p><p className="mt-1 text-xs text-muted-foreground">{(stateLabels[work.state] ? t(stateLabels[work.state]) : work.state)}</p></td><td className="max-w-64 p-3"><p>{work.summary || "—"}</p><p className="mt-2 text-muted-foreground">{work.next || "—"}</p></td><td className="max-w-56 p-3"><p>{work.gate?.title || "—"}</p>{work.waitCondition ? <p className="mt-1 text-muted-foreground">{work.waitCondition}</p> : null}{work.waitUntil ? <p className="mt-1 text-xs text-muted-foreground">{displayTime(work.waitUntil)}</p> : null}</td><td className="p-3 text-xs"><p>{t("instructor.recorded")}</p><p className="mt-1 text-muted-foreground">{displayTime(work.updatedAt)}</p><p className="mt-2">{t("instructor.progress")}</p><p className="mt-1 text-muted-foreground">{displayTime(work.progressAt)}</p>{work.verification ? <p className="mt-2 text-muted-foreground">{t("instructor.verificationStatus", { status: work.verification.status, passes: work.verification.passes, maximum: work.verification.maxPasses })}</p> : null}</td><td className="max-w-56 p-3"><div className="flex flex-wrap gap-1">{registry.filter((spec) => (work.actions ?? []).includes(spec.action) && spec.action !== "details" && spec.action !== "neu").map((spec) => <Button key={spec.action} size="sm" variant="outline" disabled={command.isPending} title={spec.description} onClick={() => run(spec, [work])}>{spec.name}</Button>)}<Button size="sm" variant="ghost" onClick={() => setDetailsId(work.id)}>{t("instructor.details")}</Button></div></td></tr>)}</tbody></table></div></section>)}
					{delta ? <section className="rounded-lg border border-border p-4"><h2 className="font-medium">{t("instructor.delta")}</h2><p className="mt-1 text-xs text-muted-foreground">{t("instructor.snapshot", { id: delta.id, time: displayTime(delta.createdAt) })}</p>{delta.works.length ? <ul className="mt-3 space-y-2 text-sm">{delta.works.map((work) => <li key={work.id}><span className="font-medium">{work.name}</span> · {work.phase} · {work.summary}</li>)}</ul> : <p className="mt-2 text-sm text-muted-foreground">{t("instructor.noDelta")}</p>}</section> : null}
					{command.data ? <section className="rounded-lg border border-border p-4" aria-live="polite"><h2 className="font-medium">{t("instructor.delivery")}</h2><p className="mt-1 text-xs text-muted-foreground">{t("instructor.deliveryHint")}</p>{command.data.dispatches?.map((dispatch) => <p key={dispatch.id} className="mt-2 text-sm">{works.find((work) => work.id === dispatch.workId)?.name ?? dispatch.workId} · {dispatch.action} · <span className="font-medium">{dispatch.state}</span>{dispatch.error ? <span className="text-destructive"> · {dispatch.error}</span> : null}</p>)}{command.data.skipped?.map((reason, index) => <p key={index} className="mt-2 text-sm text-muted-foreground">{t("instructor.skipped", { reason })}</p>)}</section> : null}
					<InstructorInventory data={inventory.data} loading={inventory.isLoading} error={inventory.error} reconciling={inventoryReconcile.isPending} reconcileError={inventoryReconcile.error} onReconcile={async (resourceId, lastSeenAt) => { await inventoryReconcile.mutateAsync({ resourceId, lastSeenAt }); await inventory.refetch(); }} />
				</> : null}
			</main>
			<Dialog open={Boolean(pending)} onOpenChange={(open) => { if (!open && !command.isPending) setPending(null); }}><DialogContent><DialogTitle>{pending?.spec.name}</DialogTitle><DialogDescription>{pending?.spec.description}</DialogDescription><form className="space-y-4" onSubmit={(event) => void submitPending(event)}><p className="text-sm">{pending?.works.map((work) => work.name).join(", ")}</p><p className="break-all text-xs text-muted-foreground">{pending?.snapshotId}</p>{pending?.works.some((work) => work.gate?.exactApproval) ? <><label className="block text-sm" htmlFor="instructor-approval">{t("instructor.exactApproval")}</label>{pending.works.filter((work) => work.gate?.exactApproval).map((work) => <p key={work.id} className="break-all rounded bg-muted p-2 font-mono text-xs">{work.gate?.exactApproval}</p>)}<Input id="instructor-approval" value={approval} disabled={command.isPending} onChange={(event) => { setApproval(event.target.value); setPending((current) => current ? { ...current, idempotencyKey: crypto.randomUUID() } : null); }} required autoComplete="off" /></> : <p className="text-sm text-muted-foreground">{t("instructor.gateApproval")}</p>}{actionError ? <p role="alert" className="text-sm text-destructive">{actionError}</p> : null}<Button disabled={command.isPending || !pending?.works.some((work) => work.gate?.exactApproval === approval)} type="submit">{pending?.spec.name}</Button></form></DialogContent></Dialog>
			<InstructorNewDialog draft={newDraft} open={newOpen} busy={command.isPending} error={actionError} projects={inventory.data?.projects ?? []} onClose={() => setNewOpen(false)} onSubmit={async (input) => { const result = await send({ command: "neu", snapshotId: newSnapshotId, new: input }); if (result) { setNewOpen(false); await query.refetch(); } }} />
			<InstructorPolicyDialog open={enabled && policyOpen} onClose={() => setPolicyOpen(false)} />
			<Dialog open={enabled && Boolean(detailsId)} onOpenChange={(open) => { if (!open) { setDetailsId(null); setExceptionOpen(false); } }}><DialogContent className="max-h-[85vh] max-w-3xl overflow-y-auto"><DialogTitle>{details.data?.work.name ?? t("instructor.details")}</DialogTitle><DialogDescription>{details.data?.work.goal ?? t("instructor.loading")}</DialogDescription>{details.error ? <p role="alert" className="text-sm text-destructive">{details.error.message}</p> : null}{details.data ? <><Button variant="outline" onClick={() => setExceptionOpen(true)} disabled={!snapshot}>{t("instructor.exception")}</Button><InstructorWorkDetails details={details.data} />{exceptionOpen ? <InstructorExceptionDialog open budgetUsage={details.data.budgetUsage} work={details.data.work} snapshotId={snapshot?.id ?? ""} onClose={() => setExceptionOpen(false)} onApplied={async () => { setExceptionNotice(true); await details.refetch(); }} /> : null}<InstructorDeliveryReconcile key={`${details.data.work.id}:${details.data.work.gate?.dispatchId ?? ""}:${snapshot?.id ?? ""}`} work={details.data.work} snapshotId={snapshot?.id ?? ""} onSaved={async () => { setReconcileNotice(true); await details.refetch(); }} /></> : null}</DialogContent></Dialog>
		</div>
	);
}


type NewInput = NonNullable<InstructorCommandInput["new"]>;

export function InstructorNewDialog({ draft, open, busy, error, projects, onClose, onSubmit }: { draft: Partial<NewInput> | null; open: boolean; busy: boolean; error: string | null; projects: Array<{ id: string; name: string }>; onClose: () => void; onSubmit: (input: NewInput) => Promise<void> }) {
	const { t } = useTranslation();
	const [projectId, setProjectId] = useState("");
	const [name, setName] = useState("");
	const [goal, setGoal] = useState("");
	const [sourceRef, setSourceRef] = useState("");
	const [issueId, setIssueId] = useState("");
	useEffect(() => { if (open) { setProjectId(draft?.projectId ?? ""); setName(draft?.name ?? ""); setGoal(draft?.goal ?? ""); setSourceRef(draft?.sourceRef ?? ""); setIssueId(draft?.issueId ?? ""); } }, [open, draft]);
	async function submit(event: FormEvent) {
		event.preventDefault();
		await onSubmit({ projectId, name: name.trim(), goal: goal.trim(), source: "operator", sourceRef: sourceRef.trim(), ...(issueId.trim() ? { issueId: issueId.trim() } : {}) });
	}
	return <Dialog open={open} onOpenChange={(next) => { if (!next && !busy) onClose(); }}><DialogContent className="max-h-[calc(100svh-2rem)] w-[min(32rem,calc(100vw-2rem))] overflow-y-auto"><DialogTitle>{t("instructor.new")}</DialogTitle><DialogDescription>{t("instructor.newHint")}</DialogDescription><form className="space-y-4" onSubmit={(event) => void submit(event)}><label className="block text-sm">{t("instructor.project")}<select aria-label={t("instructor.project")} required value={projectId} onChange={(event) => setProjectId(event.target.value)} className="mt-1 w-full rounded-md border border-input bg-background p-2"><option value="">{t("instructor.chooseProject")}</option>{projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label><label className="block text-sm" htmlFor="instructor-name">{t("instructor.name")}</label><Input id="instructor-name" required maxLength={120} value={name} onChange={(event) => setName(event.target.value)} /><label className="block text-sm" htmlFor="instructor-goal">{t("instructor.goal")}</label><textarea id="instructor-goal" required maxLength={4096} value={goal} onChange={(event) => setGoal(event.target.value)} className="min-h-24 w-full rounded-md border border-input bg-background p-2 text-sm" /><label className="block text-sm" htmlFor="instructor-issue">{t("instructor.issue")}</label><Input id="instructor-issue" value={issueId} onChange={(event) => setIssueId(event.target.value)} /><label className="block text-sm" htmlFor="instructor-source">{t("instructor.sourceRef")}</label><Input id="instructor-source" value={sourceRef} onChange={(event) => setSourceRef(event.target.value)} />{error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}<Button type="submit" disabled={busy}>{t("instructor.new")}</Button></form></DialogContent></Dialog>;
}

export function InstructorWorkDetails({ details }: { details: NonNullable<ReturnType<typeof useInstructorDetails>["data"]> }) {
	const { t } = useTranslation();
	const work = details.work;
	return <div className="space-y-5 text-sm"><p className="break-all font-mono text-xs text-muted-foreground">{work.id} · {work.projectId} · {work.sourceRef}</p><p>{t("instructor.policy", { version: work.policyVersion })}</p><section><h3 className="mb-2 font-medium">{t("instructor.roleConfiguration")}</h3><p className="mb-2 text-xs text-muted-foreground">{t("instructor.effortHint")}</p><table className="w-full text-left text-xs"><thead className="text-muted-foreground"><tr><th className="p-2">{t("instructor.role")}</th><th className="p-2">{t("instructor.harness")}</th><th className="p-2">{t("instructor.requested")}</th><th className="p-2">{t("instructor.confirmed")}</th></tr></thead><tbody>{Object.entries(work.policy.roles ?? {}).map(([role, settings]) => <tr key={role}><td className="p-2">{role}</td><td className="p-2">{settings.harness}</td><td className="p-2">{settings.model || t("instructor.unknown")} · {(settings.effort === "maximum" || settings.effort === "maximum-native" ? settings.effortByModel?.[settings.model] : settings.effort) || t("instructor.unknown")}</td><td className="p-2">{t("instructor.unknown")}</td></tr>)}</tbody></table></section><section><h3 className="mb-2 font-medium">{t("instructor.launches")}</h3><table className="w-full text-left text-xs"><thead className="text-muted-foreground"><tr><th className="p-2">{t("instructor.role")}</th><th className="p-2">{t("instructor.requested")}</th><th className="p-2">{t("instructor.sent")}</th><th className="p-2">{t("instructor.confirmed")}</th></tr></thead><tbody>{work.launches?.map((launch, index) => <tr key={launch.sessionId || launch.reviewId || index}><td className="p-2">{launch.role}<p className="mt-1 text-muted-foreground">{launch.status}</p></td><td className="p-2">{selectionText(launch.requested, t("instructor.unknown"))}</td><td className="p-2">{launch.sent ? selectionText(launch.sent, t("instructor.unknown")) : t("instructor.unknown")}</td><td className="p-2">{launch.confirmed ? selectionText(launch.confirmed, t("instructor.unknown")) : t("instructor.unknown")}</td></tr>)}</tbody></table></section><section><h3 className="mb-2 font-medium">{t("instructor.verification")}</h3>{work.verification ? <div className="space-y-2"><p>{t("instructor.verificationStatus", { status: work.verification.status, passes: work.verification.passes, maximum: work.verification.maxPasses })}</p><p className="text-xs text-muted-foreground">{t("instructor.verificationCounts", { claims: work.verification.claims, verified: work.verification.verified })}</p><p className="text-xs text-muted-foreground">{t("instructor.verificationTimes", { last: displayTime(work.verification.lastRunAt), next: displayTime(work.verification.nextRunAt) })}</p>{work.verification.error ? <p role="alert" className="text-xs text-destructive">{work.verification.error}</p> : null}</div> : <p className="text-muted-foreground">{t("instructor.noVerification")}</p>}</section><section><h3 className="mb-2 font-medium">{t("instructor.evidence")}</h3>{work.evidence?.length ? <ul className="space-y-2">{work.evidence.map((item) => <li key={item.id} className="rounded bg-muted/30 p-2"><p>{item.kind} · {item.role} · {item.profile} · {t(item.revoked ? "instructor.revoked" : item.verified ? "instructor.verified" : "instructor.unverified")}</p><p className="mt-1 text-xs text-muted-foreground">{item.phase} · {item.phaseSequence} · {item.outcome || "—"}{typeof item.criticalFindings === "number" ? <> · {t("instructor.criticalFindings", { count: item.criticalFindings })}</> : null}</p><p className="mt-1 break-all text-xs text-muted-foreground">{item.ref} · {item.sha} · {displayTime(item.createdAt)}</p>{item.revocationReason ? <p className="mt-1 text-xs text-destructive">{item.revocationReason}</p> : null}</li>)}</ul> : <p className="text-muted-foreground">{t("instructor.noEvidence")}</p>}</section><section><h3 className="mb-2 font-medium">{t("instructor.delivery")}</h3>{details.dispatches?.map((item) => <p key={item.id} className="mb-2">{item.action} · {item.state} · {displayTime(item.updatedAt)}{item.error ? <span className="text-destructive"> · {item.error}</span> : null}</p>)}</section><section><h3 className="mb-2 font-medium">{t("instructor.publicationGrants")}</h3>{work.publicationGrants?.length ? <ul className="space-y-2">{work.publicationGrants.map((grant, index) => <li key={`${grant.gateId}-${index}`} className="rounded bg-muted/30 p-2"><p className="break-all font-mono text-xs">{grant.approval}</p><p className="mt-1 break-all text-xs text-muted-foreground">{grant.sha} · {grant.policyVersion} · {grant.snapshotId} · {displayTime(grant.approvedAt)}</p></li>)}</ul> : <p className="text-muted-foreground">{t("instructor.noPublicationGrants")}</p>}</section><section><h3 className="mb-2 font-medium">{t("instructor.exceptionHistory")}</h3>{work.exceptions?.length ? <ul className="space-y-2">{work.exceptions.map((item) => <li key={item.id} className="rounded bg-muted/30 p-2"><p>{item.role || item.kind} · {item.superseded ? t("instructor.exceptionSuperseded") : t("instructor.exceptionRecorded")}</p>{item.selection ? <p className="mt-1 text-xs">{selectionText(item.selection, t("instructor.unknown"))}</p> : null}<p className="mt-1 text-xs">{item.reason}</p><p className="mt-1 text-xs text-muted-foreground">{displayTime(item.grantedAt)} · {t("instructor.exceptionExpiry")}: {displayTime(item.expiresAt)}</p><p className="mt-1 break-all font-mono text-xs text-muted-foreground">{item.beforePolicyVersion} → {item.afterPolicyVersion}</p></li>)}</ul> : <p className="text-muted-foreground">{t("instructor.noExceptions")}</p>}</section><section><h3 className="mb-2 font-medium">{t("instructor.events")}</h3>{details.events?.map((item) => <p key={item.id} className="mb-2">{displayTime(item.at)} · {item.kind} · {item.detail}</p>)}</section></div>;
}

function selectionText(selection: InstructorWork["launches"][number]["requested"], unknown = "—") {
	return [selection.harness || unknown, selection.model || unknown, selection.effort || unknown].join(" · ");
}

export function InstructorDeliveryReconcile({ work, snapshotId, onSaved }: { work: InstructorWork; snapshotId: string; onSaved: () => Promise<void> }) {
	const { t } = useTranslation();
	const reconcile = useInstructorReconcile();
	const [observation, setObservation] = useState("");
	const [note, setNote] = useState("");
	const [error, setError] = useState<string | null>(null);
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
