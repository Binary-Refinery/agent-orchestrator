import { useTranslation } from "react-i18next";
import type { InstructorWork } from "../hooks/useInstructor";
import type { MessageKey } from "../i18n/messages";

const labels: Record<string, MessageKey> = {
	intake: "instructor.board.phase.intake", implement: "instructor.board.phase.build", implementation: "instructor.board.phase.build",
	review: "instructor.board.phase.review", triage: "instructor.board.phase.triage", fix: "instructor.board.phase.fix",
	delta: "instructor.board.phase.delta", publication: "instructor.board.phase.publish", publish: "instructor.board.phase.publish",
	closeout: "instructor.board.phase.completion", done: "instructor.board.phase.completion",
};

export function InstructorWorkPhases({ work, detailed = false }: { work: InstructorWork; detailed?: boolean }) {
	const { t } = useTranslation();
	// These are workflow stations, not proof that a check passed. The daemon
	// phase remains authoritative, including backward transitions for fixes.
	const phases = [...new Set([work.policy.initialPhase, ...(work.policy.transitions ?? []).flatMap((step) => [step.from, step.to])].filter(Boolean))];
	if (!phases.includes(work.phase)) phases.push(work.phase);
	const current = phases.indexOf(work.phase);
	const phaseLabel = labels[work.phase] ? t(labels[work.phase]) : work.phase;
	return <div className="instructor-board-phases" aria-label={t("instructor.board.workflowStations")}>
		<div className="instructor-board-phase-track" role="list">{phases.map((phase, index) => <span key={phase} role="listitem" className={work.state === "done" || index < current ? "passed" : index === current ? "current" : "future"} aria-current={index === current ? "step" : undefined} title={`${labels[phase] ? t(labels[phase]) : phase} · ${t(index < current ? "instructor.board.earlierStage" : index === current ? "instructor.board.currentStage" : "instructor.board.laterStage")}`}><span className="sr-only">{labels[phase] ? t(labels[phase]) : phase}</span></span>)}</div>
		<p className="instructor-board-phase">{phaseLabel}</p>
		{detailed ? <div className="instructor-board-phase-details">{phases.map((phase, index) => <div key={phase} className={work.state === "done" || index < current ? "passed" : index === current ? "current" : "future"} aria-current={index === current ? "step" : undefined}>{labels[phase] ? t(labels[phase]) : phase}</div>)}</div> : null}
	</div>;
}
