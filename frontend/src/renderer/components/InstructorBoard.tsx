import { useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, ArrowRight, CheckCircle2, Compass, FileCheck2, Folder, Pause, Play, Plus, RefreshCw, Settings2, ShieldCheck, Users } from "lucide-react";
import {
	instructorBoardQueryKey, instructorQueryKey, useInstructorBoardSnapshot, useInstructorCapabilities,
	useInstructorCommand, useInstructorDetails, useInstructorInventory, useInstructorPolicy,
	type InstructorCommandInput, type InstructorCommandSpec, type InstructorSnapshot, type InstructorWork,
} from "../hooks/useInstructor";
import { useInstructorMembers } from "../hooks/useInstructorMembers";
import { sessionNavigateTarget } from "../lib/navigate-to-session";
import { useUiStore } from "../stores/ui-store";
import type { MessageKey } from "../i18n/messages";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./ui/dialog";
import { InstructorDeliveryReconcile, InstructorNewDialog, InstructorWorkDetails } from "./InstructorView";
import { InstructorPolicyDialog } from "./InstructorPolicyDialog";
import { InstructorModelDialog } from "./InstructorModelDialog";
import { InstructorExceptionDialog } from "./InstructorExceptionDialog";
import { InstructorPlanning } from "./InstructorPlanning";
import { ReviewerChatSurface } from "./chat/ReviewerChatSurface";
import "./instructor-board.css";

type Filter = "active" | "attention" | "blocked" | "parked" | "history";
type CapturedCommand = { spec: InstructorCommandSpec; snapshotId: string; works: InstructorWork[]; idempotencyKey: string };
type Selection = NonNullable<InstructorWork["launches"]>[number]["requested"];
const stateLabels: Record<string, MessageKey> = {
	running: "instructor.state.running", ready: "instructor.state.ready", waiting_human: "instructor.state.waiting_human",
	waiting_external: "instructor.state.waiting_external", blocked: "instructor.state.blocked", parked: "instructor.state.parked",
	done: "instructor.state.done", cancelled: "instructor.state.cancelled",
};
const filterLabels: Record<Filter, MessageKey> = {
	active: "instructor.active", attention: "instructor.board.decisions", blocked: "instructor.board.needsHelp",
	parked: "instructor.parked", history: "instructor.history",
};
function time(value?: string) {
	if (!value || value.startsWith("0001-")) return "—";
	const date = new Date(value);
	return Number.isNaN(date.getTime()) ? "—" : date.toLocaleString();
}
function matches(work: InstructorWork, filter: Filter) {
	if (filter === "history") return work.state === "done" || work.state === "cancelled";
	if (filter === "parked") return work.state === "parked";
	if (filter === "attention") return work.state === "waiting_human";
	if (filter === "blocked") return work.state === "blocked";
	return !["done", "cancelled", "parked"].includes(work.state);
}
function selectionText(value: Selection | undefined, unknown: string) {
	return value ? [value.harness || unknown, value.model || unknown, value.effort || unknown].join(" · ") : unknown;
}
function nativePolicyText(settings: NonNullable<InstructorWork["policy"]["roles"]>[string] | undefined, unknown: string) {
	return settings ? selectionText({ harness: settings.harness, model: settings.model, effort: settings.effortByModel?.[settings.model] ?? (settings.effort === "maximum" || settings.effort === "configured" || settings.effort === "maximum-native" ? "" : settings.effort) }, unknown) : unknown;
}

export function InstructorBoard({ initialWorkId, initialProjectId }: { initialWorkId?: string; initialProjectId?: string } = {}) {
	const { t } = useTranslation();
	const client = useQueryClient();
	const navigate = useNavigate();
	const openProjectSettings = useUiStore((state) => state.openProjectSettings);
	const capabilities = useInstructorCapabilities();
	const enabled = capabilities.data?.enabled === true;
	const query = useInstructorBoardSnapshot(enabled);
	const inventory = useInstructorInventory(enabled);
	const policy = useInstructorPolicy(enabled);
	const command = useInstructorCommand();
	const [filter, setFilter] = useState<Filter>("active");
	const [search, setSearch] = useState("");
	const [selected, setSelected] = useState<Set<string>>(new Set());
	const [detailsId, setDetailsId] = useState<string | null>(initialWorkId ?? null);
	const [detailTab, setDetailTab] = useState<"overview" | "members" | "history">("overview");
	const [pending, setPending] = useState<CapturedCommand | null>(null);
	const [signed, setSigned] = useState<Set<string>>(new Set());
	const [policyOpen, setPolicyOpen] = useState(false);
	const [modelOpen, setModelOpen] = useState(false);
	const [newOpen, setNewOpen] = useState(false);
	const [newDraft, setNewDraft] = useState<Partial<NonNullable<InstructorCommandInput["new"]>> | null>(null);
	const [newSnapshotId, setNewSnapshotId] = useState<string | undefined>();
	const [planningCapture, setPlanningCapture] = useState<InstructorSnapshot | null>(null);
	const [exception, setException] = useState<{ work: InstructorWork; snapshotId: string } | null>(null);
	const [reviewTarget, setReviewTarget] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [sending, setSending] = useState(false);
	const [approving, setApproving] = useState(false);
	const sendingRef = useRef(false);
	const approvingRef = useRef(false);
	const details = useInstructorDetails(enabled ? detailsId : null);
	const members = useInstructorMembers(enabled && detailTab === "members" ? detailsId : null);
	const snapshot = query.data;
	const works = snapshot?.works ?? [];
	const registry = snapshot?.commands ?? capabilities.data?.commands ?? [];
	const chosen = works.filter((work) => selected.has(work.id));
	const visible = works.filter((work) => matches(work, filter) && (!initialProjectId || work.projectId === initialProjectId) && `${work.name} ${work.goal} ${work.id}`.toLocaleLowerCase().includes(search.toLocaleLowerCase()));
	const grouped = useMemo(() => {
		const result = new Map<string, InstructorWork[]>();
		for (const work of visible) result.set(work.projectId, [...(result.get(work.projectId) ?? []), work]);
		return result;
	}, [visible]);
	const selectedWork = works.find((work) => work.id === detailsId);
	const liveExceptionWork = exception ? works.find((work) => work.id === exception.work.id) : undefined;
	const detailsExceptionWork = exception && details.data?.work.id === exception.work.id ? details.data.work : undefined;
	const observedExceptionWork = detailsExceptionWork && detailsExceptionWork.revision > (liveExceptionWork?.revision ?? -1) ? detailsExceptionWork : liveExceptionWork ?? detailsExceptionWork;
	// A provider/branch change while this modal is open never changes its intent.
	const publication = (capabilities.data as typeof capabilities.data & { publication?: { available: boolean; reason?: string } })?.publication;
	const publicationBlocked = (work: InstructorWork) => work.gate?.kind === "publication" && publication?.available !== true;

	useEffect(() => { setDetailsId(initialWorkId ?? null); setDetailTab("overview"); }, [initialWorkId]);
	useEffect(() => { setSelected((current) => new Set([...current].filter((id) => works.some((work) => work.id === id)))); }, [snapshot]);

	async function refresh() {
		await Promise.all([
			query.refetch(), inventory.refetch(),
			client.invalidateQueries({ queryKey: [...instructorQueryKey, "details"] }),
			client.invalidateQueries({ queryKey: [...instructorQueryKey, "members"] }),
		]);
	}
	async function send(body: InstructorCommandInput) {
		if (sendingRef.current) return null;
		sendingRef.current = true; setSending(true);
		setError(null);
		try {
			// Cancel an older status request before mutation; its late response must
			// not overwrite the command result. Approval inputs are captured separately.
			await client.cancelQueries({ queryKey: instructorBoardQueryKey });
			const result = await command.mutateAsync({ ...body, idempotencyKey: body.idempotencyKey ?? crypto.randomUUID() });
			if (result.snapshot) client.setQueryData(instructorBoardQueryKey, result.snapshot);
			await refresh();
			return result;
		} catch (cause) { setError(cause instanceof Error ? cause.message : t("instructor.commandFailed")); return null; }
		finally { sendingRef.current = false; setSending(false); }
	}
	function run(spec: InstructorCommandSpec, targets: InstructorWork[], snapshotId = snapshot?.id) {
		if (!snapshotId || !targets.length || targets.some((work) => !(work.actions ?? []).includes(spec.action))) return;
		if (spec.action === "go") {
			setSigned(new Set()); setError(null);
			setPending(structuredClone({ spec, snapshotId, works: targets, idempotencyKey: crypto.randomUUID() }));
		} else { void send({ command: spec.action, snapshotId, workIds: targets.map((work) => work.id) }); }
	}
	function gate(work: InstructorWork) {
		if (!snapshot) return;
		const spec = registry.find((item) => item.action === "go");
		if (!spec) return;
		setSigned(new Set()); setError(null);
		setPending(structuredClone({ spec, snapshotId: snapshot.id, works: [work], idempotencyKey: crypto.randomUUID() }));
	}
	async function approve() {
		if (!enabled || approvingRef.current || !pending || pending.works.some((work) => !signed.has(work.id) || !(work.actions ?? []).includes("go") || publicationBlocked(work))) return;
		approvingRef.current = true; setApproving(true);
		try {
			// One durable, ordered cohort lets the daemon stop every remaining
			// item after a delayed native delivery failure. HTTP acceptance is
			// only admission; it is never proof of native delivery or completion.
			const result = await send({
				command: pending.spec.action, snapshotId: pending.snapshotId,
				idempotencyKey: pending.idempotencyKey,
				workIds: pending.works.map((work) => work.id),
				approvals: pending.works.map((work) => ({ workId: work.id, approval: work.gate?.exactApproval ?? "" })),
			});
			if (!result) return;
			if (result.skipped?.length) { setError(result.skipped.join(" · ")); return; }
			setPending(null); setSelected(new Set());
		} finally {
			approvingRef.current = false; setApproving(false);
		}
	}

	function toggle(id: string) {
		setSelected((current) => { const next = new Set(current); if (next.has(id)) next.delete(id); else next.add(id); return next; });
	}
	function actionLabel(spec: InstructorCommandSpec, work?: InstructorWork) {
		if (spec.action === "weiter") return t(work?.state === "blocked" ? "instructor.board.nudge" : "instructor.board.continue");
		if (spec.action === "go") return t("instructor.board.reviewGates");
		if (spec.action === "pruefen") return t("instructor.board.verify");
		if (spec.action === "stopp") return t("instructor.board.pause");
		return spec.name;
	}
	const busy = command.isPending || sending || approving;
	return <div className="instructor-board" data-testid="instructor-board">
		<header className="instructor-board-header">
			<div><h1 className="instructor-board-title"><Compass className="size-6" aria-hidden="true" />{t("instructor.board.title")}</h1><p className="instructor-board-subtitle">{t("instructor.board.description")}</p></div>
			{enabled ? <div className="flex min-w-0 flex-wrap items-center gap-2">
				<button className="instructor-board-config" onClick={() => setModelOpen(true)}><Settings2 className="size-4 shrink-0" aria-hidden="true" /><span><span className="text-xs text-muted-foreground">{t("instructor.board.instructorModel")}</span><strong>{nativePolicyText(policy.data?.roles?.instructor, t("instructor.unknown"))}</strong></span></button>
				<Button variant="outline" aria-label={t("instructor.board.refresh")} disabled={busy || query.isFetching} onClick={() => void refresh()}><RefreshCw className="size-4" aria-hidden="true" /></Button>
				<Button variant="outline" onClick={() => setPolicyOpen(true)}>{t("instructor.configure")}</Button>
				<Button variant="outline" disabled={!snapshot} onClick={() => snapshot && setPlanningCapture(structuredClone(snapshot))}>{t("instructor.strategy")}</Button>
				{registry.some((item) => item.action === "neu") ? <Button onClick={() => { setNewDraft(null); setNewSnapshotId(snapshot?.id); setNewOpen(true); }} disabled={busy}><Plus className="size-4" aria-hidden="true" />{t("instructor.new")}</Button> : null}
			</div> : null}
		</header>
		<main className="instructor-board-main">
			{capabilities.isLoading || (enabled && query.isLoading) ? <p>{t("instructor.loading")}</p> : null}
			{capabilities.error ? <p role="alert" className="text-destructive">{t("instructor.unavailable")} {capabilities.error.message}</p> : null}
			{capabilities.data && !enabled ? <section className="rounded-xl border border-border p-5"><h2>{t("instructor.disabled")}</h2><p className="mt-2 text-muted-foreground">{capabilities.data.reason || t("instructor.enableHint")}</p></section> : null}
			{enabled && query.error ? <p role="alert" className="text-destructive">{query.error.message}</p> : null}
			{inventory.error ? <p role="alert" className="text-destructive">{inventory.error.message}</p> : null}
			{error ? <p role="alert" className="rounded-lg bg-destructive/10 p-3 text-sm text-destructive">{error}</p> : null}
			{enabled && snapshot ? <>
				<div className="instructor-board-stats" aria-label={t("instructor.metrics")}>
					<button className="instructor-board-stat" aria-pressed={filter === "active"} onClick={() => setFilter("active")}><dl><dt>{t("instructor.board.activeWorks")}</dt><dd>{snapshot.metrics?.active ?? works.filter((work) => matches(work, "active")).length}</dd></dl><Folder className="size-6 text-muted-foreground" aria-hidden="true" /></button>
					<button className="instructor-board-stat" aria-pressed={filter === "attention"} onClick={() => setFilter("attention")}><dl><dt>{t("instructor.board.decisions")}</dt><dd>{snapshot.metrics?.gated ?? works.filter((work) => matches(work, "attention")).length}</dd></dl><FileCheck2 className="size-6 text-warning" aria-hidden="true" /></button>
					<button className="instructor-board-stat" aria-pressed={filter === "blocked"} onClick={() => setFilter("blocked")}><dl><dt>{t("instructor.board.needsHelp")}</dt><dd>{snapshot.metrics?.stalled ?? works.filter((work) => matches(work, "blocked")).length}</dd></dl><AlertTriangle className="size-6 text-destructive" aria-hidden="true" /></button>
					<div className="instructor-board-stat"><dl><dt>{t("instructor.closedLast7Days")}</dt><dd>{snapshot.metrics?.closedLast7Days ?? "—"}</dd></dl><CheckCircle2 className="size-6 text-success" aria-hidden="true" /></div>
				</div>
				<div className="instructor-board-toolbar"><div className="instructor-board-filters" role="tablist" aria-label={t("instructor.sections")}>{(Object.keys(filterLabels) as Filter[]).map((item) => <Button key={item} size="sm" role="tab" aria-selected={filter === item} variant={filter === item ? "secondary" : "ghost"} onClick={() => { setFilter(item); setSelected(new Set()); }}>{t(filterLabels[item])}</Button>)}</div><Input className="instructor-board-search" aria-label={t("instructor.board.search")} placeholder={t("instructor.board.search")} value={search} onChange={(event) => setSearch(event.target.value)} /></div>
				{chosen.length ? <section className="instructor-board-batch" aria-label={t("instructor.board.batch")}><strong>{t("instructor.selected", { count: chosen.length })}</strong>{registry.filter((spec) => spec.mutates && spec.action !== "neu" && chosen.every((work) => work.actions?.includes(spec.action))).map((spec) => <Button key={spec.action} variant={spec.action === "go" ? "primary" : "outline"} disabled={busy} onClick={() => run(spec, chosen)}>{actionLabel(spec)}</Button>)}<Button variant="ghost" onClick={() => setSelected(new Set())}>{t("instructor.board.clearSelection")}</Button></section> : null}
				{snapshot.commandConflicts?.length ? <p role="alert" className="rounded-lg border border-border p-3 text-sm">{t("instructor.commandConflicts")} {snapshot.commandConflicts.join(" · ")}</p> : null}
				{!visible.length ? <p className="rounded-xl border border-dashed border-border p-8 text-center text-muted-foreground">{t("instructor.empty")}</p> : null}
				<div className="instructor-board-projects">{[...grouped].map(([projectId, projectWorks]) => <section key={projectId} className="instructor-board-project" aria-label={inventory.data?.projects.find((project) => project.id === projectId)?.name ?? projectId}>
					<h2 className="instructor-board-project-name"><span className="flex items-center gap-2"><Folder className="size-4 shrink-0" aria-hidden="true" />{inventory.data?.projects.find((project) => project.id === projectId)?.name ?? projectId}</span><span aria-hidden="true" className="text-xs font-normal text-muted-foreground">{projectWorks.length}</span></h2>
					<button className="flex w-full items-center justify-between gap-2 text-left text-xs text-muted-foreground hover:text-foreground" onClick={() => openProjectSettings(projectId, "agents")}><span>{t("instructor.board.projectOrchestrator")}</span><Settings2 className="size-4 shrink-0" aria-hidden="true" /></button>
					{projectWorks.map((work) => <article key={work.id} className="instructor-board-work" aria-label={work.name}>
						<div className="instructor-board-work-top"><label className="flex items-center gap-2 text-xs text-muted-foreground"><input className="instructor-board-select" type="checkbox" aria-label={t("instructor.selectWork", { name: work.name })} checked={selected.has(work.id)} onChange={() => toggle(work.id)} /><span>{work.issueId || work.sourceRef || work.id}</span></label><span className="instructor-board-state" data-state={work.state}>{stateLabels[work.state] ? t(stateLabels[work.state]) : work.state}</span></div>
						<h3><button className="instructor-board-work-title" onClick={() => { setDetailsId(work.id); setDetailTab("overview"); }}>{work.name}</button></h3>
						<p className="instructor-board-phase"><ArrowRight className="size-3 shrink-0" aria-hidden="true" />{work.phase}</p>
						<p className="instructor-board-next">{work.next || work.summary || work.goal}</p>
						{work.waitCondition ? <p className="instructor-board-next rounded-md border border-border p-2">{work.waitCondition}</p> : null}{work.gate ? <div className="instructor-board-gate"><p className="font-medium">{work.gate.title}</p><Button variant="ghost" size="sm" className="mt-2" onClick={() => gate(work)}><ShieldCheck className="size-4" aria-hidden="true" />{t("instructor.board.reviewGate")}</Button></div> : null}
						<div className="instructor-board-work-actions">{registry.filter((spec) => spec.mutates && spec.action !== "neu" && spec.action !== "go" && work.actions?.includes(spec.action)).map((spec) => <Button key={spec.action} variant={spec.action === "weiter" ? "primary" : "outline"} size="sm" disabled={busy} onClick={() => run(spec, [work])}>{spec.action === "weiter" ? <Play className="size-3" aria-hidden="true" /> : spec.action === "stopp" ? <Pause className="size-3" aria-hidden="true" /> : <RefreshCw className="size-3" aria-hidden="true" />}{actionLabel(spec, work)}</Button>)}<Button size="sm" variant="ghost" onClick={() => { setDetailsId(work.id); setDetailTab("members"); }} aria-label={t("instructor.board.workMembers", { name: work.name })}><Users className="size-4" aria-hidden="true" /></Button></div>
						<div className="instructor-board-footer"><span title={time(work.progressAt)}>{t("instructor.progress")}: {time(work.progressAt)}</span>{work.waitUntil ? <span>{t("instructor.board.waitUntil", { time: time(work.waitUntil) })}</span> : null}</div>
					</article>)}
				</section>)}</div>
				{command.data ? <section aria-live="polite" className="rounded-xl border border-border p-4"><h2 className="text-sm font-medium">{t("instructor.board.admissionReceipt")}</h2><p className="mt-1 text-xs text-muted-foreground">{t("instructor.board.admissionHint")}</p>{command.data.dispatches?.map((item) => <p key={item.id} className="mt-2 text-sm">{works.find((work) => work.id === item.workId)?.name ?? item.workId} · {item.state}{item.error ? <span className="text-destructive"> · {item.error}</span> : null}</p>)}{command.data.skipped?.map((reason, index) => <p key={index} className="mt-2 text-sm text-muted-foreground">{t("instructor.skipped", { reason })}</p>)}</section> : null}
				<p className="text-xs text-muted-foreground">{t("instructor.board.live", { time: time(snapshot.createdAt) })}</p>
			</> : null}
		</main>
		<Dialog open={Boolean(pending)} onOpenChange={(open) => { if (!open && !busy) setPending(null); }}><DialogContent className="instructor-board-dialog"><DialogTitle>{t("instructor.board.reviewGates")}</DialogTitle><DialogDescription>{t("instructor.board.capturedApproval")}</DialogDescription>{pending ? <>
			<p className="text-xs text-muted-foreground">{t("instructor.snapshot", { id: pending.snapshotId, time: "—" })}</p>
			{pending.works.map((work) => <section key={work.id} className="instructor-board-gate-preview"><h3 className="font-semibold">{work.name}</h3><p className="mt-1 text-sm">{work.gate?.title || work.waitCondition}</p><dl><div><dt>{t("instructor.board.branch")}</dt><dd>{work.branch || t("instructor.unknown")}</dd></div><div><dt>{t("instructor.board.commit")}</dt><dd className="font-mono">{work.sha || t("instructor.unknown")}</dd></div><div><dt>{t("instructor.policy", { version: work.policyVersion })}</dt><dd>{work.id} · {t("instructor.board.revision", { revision: work.revision })}</dd></div></dl>
				{work.evidence?.length ? <ul className="mb-3 space-y-1 text-xs">{work.evidence.map((item) => <li key={item.id} className="flex items-start gap-2">{item.verified && !item.revoked ? <CheckCircle2 className="size-3 shrink-0 text-success" aria-hidden="true" /> : <AlertTriangle className="size-3 shrink-0 text-warning" aria-hidden="true" />}<span>{item.kind} · {item.profile || item.role} · {t(item.revoked ? "instructor.revoked" : item.verified ? "instructor.verified" : "instructor.unverified")} · {item.sha}</span></li>)}</ul> : <p className="mb-3 text-xs text-muted-foreground">{t("instructor.noEvidence")}</p>}
				{publicationBlocked(work) ? <p role="alert" className="mb-3 text-sm text-destructive">{publication?.reason || t("instructor.board.publicationUnavailable")}</p> : null}
				{work.gate?.exactApproval ? <p className="mb-3 rounded-md bg-muted p-2 font-mono text-xs">{work.gate.exactApproval}</p> : null}
				{work.actions?.includes("go") ? <label className="flex items-start gap-2 text-sm"><input className="instructor-board-select mt-1" type="checkbox" disabled={busy || !enabled || publicationBlocked(work)} checked={signed.has(work.id)} aria-label={t("instructor.board.signWork", { name: work.name })} onChange={() => setSigned((current) => { const next = new Set(current); if (next.has(work.id)) next.delete(work.id); else next.add(work.id); return next; })} />{t("instructor.board.signScope")}</label> : <p className="text-xs text-muted-foreground">{t("instructor.board.notAdmitted")}</p>}
				<InstructorDeliveryReconcile work={work} snapshotId={pending.snapshotId} onSaved={async () => { await refresh(); setPending(null); }} />
			</section>)}
			{error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
			<Button disabled={busy || !enabled || pending.works.some((work) => !signed.has(work.id) || !work.actions?.includes("go") || publicationBlocked(work))} onClick={() => void approve()}><FileCheck2 className="size-4" aria-hidden="true" />{t("instructor.board.signContinue")}</Button>
		</> : null}</DialogContent></Dialog>
		<Dialog open={Boolean(detailsId)} onOpenChange={(open) => { if (!open) setDetailsId(null); }}><DialogContent className="instructor-board-dialog"><DialogTitle>{details.data?.work.name ?? selectedWork?.name ?? t("instructor.details")}</DialogTitle><DialogDescription>{details.data?.work.goal ?? selectedWork?.goal ?? t("instructor.loading")}</DialogDescription><div role="tablist" className="instructor-board-detail-tabs" aria-label={t("instructor.board.workDetails")}>{(["overview", "members", "history"] as const).map((tab) => <Button key={tab} role="tab" size="sm" aria-selected={detailTab === tab} variant={detailTab === tab ? "secondary" : "ghost"} onClick={() => setDetailTab(tab)}>{t(tab === "overview" ? "instructor.board.overview" : tab === "members" ? "instructor.board.members" : "instructor.events")}</Button>)}</div>
			{details.isLoading ? <p>{t("instructor.loading")}</p> : null}{details.error ? <p role="alert" className="text-destructive">{details.error.message}</p> : null}
			{detailTab === "overview" && details.data ? <section className="space-y-4 text-sm"><p>{details.data.work.summary}</p><p><strong>{t("instructor.board.next")}</strong> {details.data.work.next}</p>{details.data.work.waitCondition ? <section className="rounded-lg border border-border p-3"><h3 className="font-medium">{t("instructor.gateWait")}</h3><p className="mt-1">{details.data.work.waitCondition}</p>{details.data.work.waitUntil ? <p className="mt-1 text-xs text-muted-foreground">{t("instructor.board.waitUntil", { time: time(details.data.work.waitUntil) })}</p> : null}</section> : null}{selectedWork?.gate ? <Button variant="outline" onClick={() => gate(selectedWork)}>{t("instructor.board.reviewGate")}</Button> : null}<h3 className="font-medium">{t("instructor.roleConfiguration")}</h3><p className="text-xs text-muted-foreground">{t("instructor.effortHint")}</p><dl className="instructor-board-native">{Object.entries(details.data.work.policy.roles ?? {}).map(([role, settings]) => <div key={role}><dt>{role}</dt><dd>{nativePolicyText(settings, t("instructor.unknown"))}</dd></div>)}</dl>{selectedWork && snapshot ? <Button variant="outline" onClick={() => setException(structuredClone({ work: selectedWork, snapshotId: snapshot.id }))}>{t("instructor.board.workModels")}</Button> : null}<p className="text-xs text-muted-foreground">{t("instructor.board.workModelHint")}</p>{details.data.dispatches?.length ? <section><h3 className="font-medium">{t("instructor.delivery")}</h3><ul className="mt-2 space-y-2 text-xs">{details.data.dispatches.map((item) => <li key={item.id}><p>{item.action} · {item.state} · {time(item.updatedAt)}</p>{item.error ? <p className="text-destructive">{item.error}</p> : null}<p className="mt-1 break-all text-muted-foreground">{item.batchId} · {item.id}</p></li>)}</ul></section> : null}<dl className="grid gap-3 text-xs"><div><dt className="text-muted-foreground">{t("instructor.recorded")}</dt><dd>{time(details.data.work.updatedAt)}</dd></div><div><dt className="text-muted-foreground">{t("instructor.progress")}</dt><dd>{time(details.data.work.progressAt)}</dd></div><div><dt className="text-muted-foreground">{t("instructor.board.heartbeat")}</dt><dd>{time(details.data.work.heartbeat?.at)}</dd></div><div><dt className="text-muted-foreground">{t("instructor.verification")}</dt><dd>{details.data.work.verification?.status || t("instructor.unknown")} · {time(details.data.work.verification?.lastRunAt)}</dd></div></dl></section> : null}
			{detailTab === "history" && details.data ? <InstructorWorkDetails details={details.data} /> : null}
			{detailTab === "members" ? <section className="space-y-3">{members.isLoading ? <p>{t("instructor.loading")}</p> : null}{members.error ? <p role="alert" className="text-destructive">{members.error.message}</p> : null}{members.data ? <><p className="text-xs text-muted-foreground">{t("instructor.board.membersCaptured", { time: time(members.data.capturedAt), revision: members.data.workRevision })}</p>{[...(members.data.coordinator.current ? [members.data.coordinator.current] : []), ...members.data.members.filter((member) => member.id !== members.data?.coordinator.current?.id)].map((member) => <article key={member.id} className="rounded-lg border border-border p-3"><div className="flex flex-wrap items-center justify-between gap-2"><div><h3 className="font-medium">{member.name}</h3><p className="mt-1 text-xs text-muted-foreground">{member.role} · {member.status} · {t(member.association === "current" ? "instructor.board.memberCurrent" : member.association === "historical" ? "instructor.board.memberHistorical" : "instructor.board.memberUnknown")}</p></div>{member.openTarget ? <Button size="sm" variant="outline" onClick={() => { if (member.openTarget?.kind === "review" && member.openTarget.reviewId) setReviewTarget(member.openTarget.reviewId); else if (member.openTarget?.kind === "session") void navigate(sessionNavigateTarget(member.openTarget.projectId, member.openTarget.sessionId)); }}>{t("instructor.board.openAgent")}</Button> : null}</div><dl className="instructor-board-native mt-3">{(["requested", "sent", "confirmed"] as const).map((field) => <div key={field}><dt>{t(`instructor.${field}`)}</dt><dd>{selectionText(member[field], t("instructor.unknown"))}</dd></div>)}</dl>{member.reason ? <p className="mt-2 text-xs text-muted-foreground">{member.reason}</p> : null}</article>)}{!members.data.members.length && !members.data.coordinator.current ? <p className="text-sm text-muted-foreground">{t("instructor.board.noMembers")}</p> : null}</> : null}</section> : null}
		</DialogContent></Dialog>
		<Dialog open={Boolean(reviewTarget)} onOpenChange={(open) => { if (!open) setReviewTarget(null); }}><DialogContent className="instructor-board-dialog h-[80svh]"><DialogTitle>{t("terminal.reviewer")}</DialogTitle><DialogDescription>{t("instructor.board.reviewConversation")}</DialogDescription>{reviewTarget ? <div className="min-h-0 flex-1"><ReviewerChatSurface reviewId={reviewTarget} /></div> : null}</DialogContent></Dialog>
		<Dialog open={Boolean(planningCapture)} onOpenChange={(open) => { if (!open) setPlanningCapture(null); }}><DialogContent className="instructor-board-dialog"><DialogTitle>{t("instructor.strategy")}</DialogTitle><DialogDescription>{t("instructor.strategyHint")}</DialogDescription>{planningCapture ? <InstructorPlanning snapshotId={planningCapture.id} busy={busy} onProposal={(job, proposal) => {
			if (job.input.snapshotId !== planningCapture.id) return;
			const name = proposal.command.toLocaleLowerCase();
			const spec = planningCapture.commands.find((item) => [item.action, item.name, ...(item.aliases ?? [])].some((alias) => alias.toLocaleLowerCase() === name));
			if (!spec) { setError(t("instructor.strategyProposalUnavailable")); return; }
			if (["status", "delta"].includes(spec.action)) { void refresh(); return; }
			if (spec.action === "tore") { setFilter("attention"); setPlanningCapture(null); return; }
			const targets = planningCapture.works.filter((work) => proposal.workIds.includes(work.id));
			if (!targets.length || targets.length !== proposal.workIds.length || targets.some((work) => !work.actions?.includes(spec.action))) { setError(t("instructor.strategyProposalUnavailable")); return; }
			if (spec.action === "details") { setDetailsId(targets[0].id); setDetailTab("overview"); setPlanningCapture(null); return; }
			run(spec, targets, planningCapture.id); setPlanningCapture(null);
		}} onIntake={(job, intake) => { if (job.input.snapshotId !== planningCapture.id) return; setNewDraft({ ...intake, sourceRef: `plan:${job.id}:${job.output?.intake.indexOf(intake) ?? -1}` }); setNewSnapshotId(job.input.snapshotId); setNewOpen(true); setPlanningCapture(null); }} /> : null}</DialogContent></Dialog>
		<InstructorNewDialog draft={newDraft} open={newOpen} busy={busy} error={error} projects={(inventory.data?.projects ?? []).map((project) => ({ id: project.id, name: project.name }))} onClose={() => setNewOpen(false)} onSubmit={async (input) => { const result = await send({ command: "neu", snapshotId: newSnapshotId, new: input }); if (result) setNewOpen(false); }} />
		<InstructorPolicyDialog open={policyOpen} onClose={() => setPolicyOpen(false)} />
		<InstructorModelDialog open={modelOpen} onClose={() => setModelOpen(false)} />
		{exception ? <InstructorExceptionDialog open clickApproval observedWork={observedExceptionWork} budgetUsage={details.data?.work.id === exception.work.id ? details.data.budgetUsage : undefined} work={exception.work} snapshotId={exception.snapshotId} onClose={() => setException(null)} onApplied={refresh} /> : null}
	</div>;
}
