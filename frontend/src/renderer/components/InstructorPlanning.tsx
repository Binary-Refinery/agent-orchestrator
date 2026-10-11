import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useCreateInstructorPlan, useInstructorPlans, type InstructorPlanJob, type InstructorPlanIntake, type InstructorProposal } from "../hooks/useInstructor";
import { Button } from "./ui/button";

function selectionText(selection: InstructorPlanJob["requested"] | undefined, unknown: string) {
	return selection ? [selection.harness || unknown, selection.model || unknown, selection.effort || unknown].join(" · ") : unknown;
}

export function InstructorPlanning({ snapshotId, busy, onProposal, onIntake }: { snapshotId: string; busy: boolean; onProposal: (job: InstructorPlanJob, proposal: InstructorProposal) => void; onIntake: (job: InstructorPlanJob, intake: InstructorPlanIntake) => void }) {
	const { t } = useTranslation();
	const plans = useInstructorPlans();
	const create = useCreateInstructorPlan();
	const [request, setRequest] = useState("");
	const [activeId, setActiveId] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);
	async function submit(event: FormEvent) {
		event.preventDefault();
		if (!request.trim() || !snapshotId) return;
		setError(null);
		try {
			const job = await create.mutateAsync({ request: request.trim(), snapshotId, idempotencyKey: crypto.randomUUID() });
			setActiveId(job.id);
			setRequest("");
			await plans.refetch();
		} catch (cause) { setError(cause instanceof Error ? cause.message : t("instructor.commandFailed")); }
	}
	const jobs = [...(plans.data?.jobs ?? [])].sort((left, right) => right.createdAt.localeCompare(left.createdAt));
	return <section className="rounded-lg border border-border p-4" aria-label={t("instructor.strategy")}>
		<h2 className="font-medium">{t("instructor.strategy")}</h2>
		<p className="mt-1 text-xs text-muted-foreground">{t("instructor.strategyHint")}</p>
		<form className="mt-3 flex flex-col gap-2" onSubmit={(event) => void submit(event)}>
			<label className="sr-only" htmlFor="instructor-strategic-request">{t("instructor.strategyRequest")}</label>
			<textarea id="instructor-strategic-request" required maxLength={8192} rows={2} value={request} onChange={(event) => setRequest(event.target.value)} placeholder={t("instructor.strategyPlaceholder")} className="w-full resize-y rounded-md border border-input bg-background p-2 text-sm" />
			<div className="flex flex-wrap items-center justify-between gap-2"><span className="break-all text-xs text-muted-foreground">{snapshotId}</span><Button type="submit" variant="outline" disabled={create.isPending || !snapshotId || !request.trim()}>{t("instructor.strategySubmit")}</Button></div>
		</form>
		{error || plans.error ? <p role="alert" className="mt-3 text-sm text-destructive">{error || plans.error?.message}</p> : null}
		{plans.isLoading ? <p className="mt-3 text-sm text-muted-foreground">{t("instructor.loading")}</p> : null}
		{jobs.length ? <div className="mt-4 space-y-2">{jobs.map((job) => <details key={job.id} open={job.id === activeId} className="rounded-md border border-border p-3">
			<summary className="cursor-pointer text-sm"><span className="font-medium">{job.input.request}</span><span className="ml-2 text-muted-foreground">{job.state}</span></summary>
			<p className="mt-2 break-all font-mono text-[11px] text-muted-foreground">{job.id} · {job.input.snapshotId} · {job.policyVersion}</p>
			<dl className="mt-3 grid gap-2 text-xs sm:grid-cols-3">{(["requested", "sent", "confirmed"] as const).map((field) => <div key={field}><dt className="text-muted-foreground">{t(`instructor.${field}`)}</dt><dd className="mt-1 break-all">{selectionText(job[field], t("instructor.unknown"))}</dd></div>)}</dl>
			{job.error ? <p role="alert" className="mt-3 text-sm text-destructive">{job.error}</p> : null}
			{job.output ? <div className="mt-3 space-y-3 text-sm"><p className="whitespace-pre-wrap">{job.output.summary}</p>{job.output.questions?.length ? <ul className="list-disc space-y-1 pl-4">{job.output.questions.map((question, index) => <li key={index}>{question}</li>)}</ul> : null}
				{job.input.snapshotId !== snapshotId ? <p className="text-xs text-muted-foreground">{t("instructor.strategyOldSnapshot")}</p> : null}
				{job.output.proposals?.map((proposal, index) => <div key={index} className="flex flex-wrap items-center justify-between gap-2 rounded bg-muted/30 p-2"><div><p>{proposal.command} · {proposal.workIds?.join(", ") || "—"}</p><p className="mt-1 text-xs text-muted-foreground">{proposal.reason}</p></div><Button size="sm" variant="outline" disabled={busy || job.input.snapshotId !== snapshotId} onClick={() => onProposal(job, proposal)}>{t("instructor.strategyReviewCommand")}</Button></div>)}
				{job.output.intake?.map((intake, index) => <div key={index} className="flex flex-wrap items-center justify-between gap-2 rounded bg-muted/30 p-2"><div><p>{intake.name} · {intake.projectId}</p><p className="mt-1 text-xs text-muted-foreground">{intake.goal}</p></div><Button size="sm" variant="outline" disabled={busy || job.input.snapshotId !== snapshotId} onClick={() => onIntake(job, intake)}>{t("instructor.strategyReviewIntake")}</Button></div>)}
			</div> : null}
		</details>)}</div> : <p className="mt-3 text-xs text-muted-foreground">{t("instructor.strategyEmpty")}</p>}
	</section>;
}
