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
	const [error, setError] = useState<string | null>(null);
	async function submit(event: FormEvent) {
		event.preventDefault();
		if (!request.trim() || !snapshotId) return;
		setError(null);
		try {
			await create.mutateAsync({ request: request.trim(), snapshotId, idempotencyKey: crypto.randomUUID() });
			setRequest("");
			await plans.refetch();
		} catch (cause) { setError(cause instanceof Error ? cause.message : t("instructor.commandFailed")); }
	}
	const jobs = [...(plans.data?.jobs ?? [])].sort((left, right) => right.createdAt.localeCompare(left.createdAt));
	return <section className="instructor-planning" aria-label={t("instructor.strategy")}>
		<h2 className="font-medium">{t("instructor.strategy")}</h2>
		<p className="mt-1 text-xs text-muted-foreground">{t("instructor.strategyHint")}</p>
		<form className="mt-3 flex flex-col gap-2" onSubmit={(event) => void submit(event)}>
			<label className="sr-only" htmlFor="instructor-strategic-request">{t("instructor.strategyRequest")}</label>
			<textarea id="instructor-strategic-request" required maxLength={8192} rows={2} value={request} onChange={(event) => setRequest(event.target.value)} placeholder={t(jobs.length ? "instructor.strategyFollowup" : "instructor.strategyPlaceholder")} className="w-full resize-y rounded-md border border-input bg-background p-2 text-sm" />
			<div className="flex justify-end"><Button type="submit" variant="outline" disabled={create.isPending || !snapshotId || !request.trim()}>{t("instructor.strategySubmit")}</Button></div>
		</form>
		{error || plans.error ? <p role="alert" className="mt-3 text-sm text-destructive">{error || plans.error?.message}</p> : null}
		{plans.isLoading ? <p className="mt-3 text-sm text-muted-foreground">{t("instructor.loading")}</p> : null}
		<p className="mt-3 text-xs text-muted-foreground">{t("instructor.strategyAdvisory")}</p>
		{jobs.length ? <div className="instructor-planning-conversation">{jobs.map((job) => <article key={job.id} className="instructor-planning-exchange">
			<div className="instructor-planning-question"><h3>{t("instructor.strategyQuestion")}</h3><p className="whitespace-pre-wrap">{job.input.request}</p></div>
			<div className="instructor-planning-answer"><h3>{t("instructor.strategyAnswer")}</h3>{job.error ? <p role="alert" className="text-destructive">{job.error}</p> : null}
			{job.output ? <div className="space-y-3 text-sm"><p className="whitespace-pre-wrap">{job.output.summary}</p>{job.output.questions?.length ? <ul className="list-disc space-y-1 pl-4">{job.output.questions.map((question, index) => <li key={index}>{question}</li>)}</ul> : null}
				{job.input.snapshotId !== snapshotId ? <p className="text-xs text-muted-foreground">{t("instructor.strategyOldSnapshot")}</p> : null}
				{job.output.proposals?.length ? <h4 className="font-medium">{t("instructor.strategyProposals")}</h4> : null}
				{job.output.proposals?.map((proposal, index) => <div key={index} className="flex flex-wrap items-center justify-between gap-2 rounded bg-muted/30 p-2"><div><p>{proposal.command} · {proposal.workIds?.join(", ") || "—"}</p><p className="mt-1 text-xs text-muted-foreground">{proposal.reason}</p></div><Button size="sm" variant="outline" disabled={busy || job.input.snapshotId !== snapshotId} onClick={() => onProposal(job, proposal)}>{t("instructor.strategyReviewCommand")}</Button></div>)}
				{job.output.intake?.length ? <h4 className="font-medium">{t("instructor.strategyIntakes")}</h4> : null}
				{job.output.intake?.map((intake, index) => <div key={index} className="flex flex-wrap items-center justify-between gap-2 rounded bg-muted/30 p-2"><div><p>{intake.name} · {intake.projectId}</p><p className="mt-1 text-xs text-muted-foreground">{intake.goal}</p></div><Button size="sm" variant="outline" disabled={busy || job.input.snapshotId !== snapshotId} onClick={() => onIntake(job, intake)}>{t("instructor.strategyReviewIntake")}</Button></div>)}
			</div> : !job.error ? <p className="text-sm text-muted-foreground">{t("instructor.strategyAwaiting")} · {job.state}</p> : null}</div>
			<details className="instructor-planning-technical"><summary>{t("instructor.strategyDetails")}</summary><p className="mt-2 break-all font-mono text-[11px] text-muted-foreground">{job.id} · {job.input.snapshotId} · {job.policyVersion}</p><dl className="mt-3 grid gap-2 text-xs sm:grid-cols-3">{(["requested", "sent", "confirmed"] as const).map((field) => <div key={field}><dt className="text-muted-foreground">{t(`instructor.${field}`)}</dt><dd className="mt-1 break-all">{selectionText(job[field], t("instructor.unknown"))}</dd></div>)}</dl></details>
		</article>)}</div> : <p className="mt-3 text-xs text-muted-foreground">{t("instructor.strategyEmpty")}</p>}
	</section>;
}
