import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import type { InstructorCommandInput } from "../hooks/useInstructor";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./ui/dialog";

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
	return <Dialog open={open} onOpenChange={(next) => { if (!next && !busy) onClose(); }}><DialogContent className="instructor-board-dialog max-h-[calc(100svh-2rem)] w-[min(32rem,calc(100vw-2rem))] overflow-y-auto"><DialogTitle>{t("instructor.new")}</DialogTitle><DialogDescription>{t("instructor.newHint")}</DialogDescription><form className="space-y-4" onSubmit={(event) => void submit(event)}><label className="block text-sm">{t("instructor.project")}<select aria-label={t("instructor.project")} required value={projectId} onChange={(event) => setProjectId(event.target.value)} className="mt-1 w-full rounded-md border border-input bg-background p-2"><option value="">{t("instructor.chooseProject")}</option>{projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label><label className="block text-sm" htmlFor="instructor-name">{t("instructor.name")}</label><Input id="instructor-name" required maxLength={120} value={name} onChange={(event) => setName(event.target.value)} /><label className="block text-sm" htmlFor="instructor-goal">{t("instructor.goal")}</label><textarea id="instructor-goal" required maxLength={4096} value={goal} onChange={(event) => setGoal(event.target.value)} className="min-h-24 w-full rounded-md border border-input bg-background p-2 text-sm" /><label className="block text-sm" htmlFor="instructor-issue">{t("instructor.issue")}</label><Input id="instructor-issue" value={issueId} onChange={(event) => setIssueId(event.target.value)} /><label className="block text-sm" htmlFor="instructor-source">{t("instructor.sourceRef")}</label><Input id="instructor-source" value={sourceRef} onChange={(event) => setSourceRef(event.target.value)} />{error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}<Button type="submit" disabled={busy}>{t("instructor.new")}</Button></form></DialogContent></Dialog>;
}

