import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { apiErrorMessage, apiClient } from "../lib/api-client";
import type { components } from "../../api/schema";
import type { InstructorWork } from "../hooks/useInstructor";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./ui/dialog";

type Issues = components["schemas"]["InstructorIssueCandidates"];

export function InstructorIssueIntake({ projectId, onClose, onImported }: { projectId: string | null; onClose: () => void; onImported: (work: InstructorWork) => Promise<void> }) {
	const { t } = useTranslation();
	const [number, setNumber] = useState<number | null>(null);
	const [lookup, setLookup] = useState("");
	const [previewNumber, setPreviewNumber] = useState<number | null>(null);
	const submitting = useRef(false);
	const activeProject = useRef(projectId);
	activeProject.current = projectId;
	useEffect(() => () => { activeProject.current = null; }, []);
	const [name, setName] = useState("");
	const [error, setError] = useState<string | null>(null);
	useEffect(() => { setNumber(null); setName(""); setError(null); setLookup(""); setPreviewNumber(null); }, [projectId]);
	const issues = useQuery({ queryKey: ["instructor", "issues", projectId, previewNumber], enabled: Boolean(projectId), retry: false, queryFn: async ({ signal }) => {
		if (!projectId) throw new Error(t("instructor.board.issueUnavailable"));
		const { data, error } = await apiClient.GET("/api/v1/instructor/projects/{projectId}/issues", { params: { path: { projectId }, ...(previewNumber ? { query: { number: previewNumber } } : {}) }, signal });
		if (error) throw new Error(apiErrorMessage(error));
		if (!data) throw new Error(t("instructor.board.issueUnavailable"));
		return data as Issues;
	} });
	const selected = issues.data?.issues.find((issue) => issue.number === number);
	useEffect(() => { if (previewNumber && issues.data?.issues.some((issue) => issue.number === previewNumber)) setNumber(previewNumber); }, [previewNumber, issues.data]);
	const intake = useMutation({ mutationFn: async () => {
		if (!projectId || !issues.data || !selected) throw new Error(t("instructor.board.issueUnavailable"));
		const { data, error } = await apiClient.POST("/api/v1/instructor/projects/{projectId}/issues/intake", { params: { path: { projectId } }, body: { repository: issues.data.repository, number: selected.number, ...(name.trim() ? { name: name.trim() } : {}) } });
		if (error) throw new Error(apiErrorMessage(error));
		if (!data) throw new Error(t("instructor.board.issueUnavailable"));
		return data as InstructorWork;
	}, retry: false });
	return <Dialog open={Boolean(projectId)} onOpenChange={(open) => { if (!open && !intake.isPending) onClose(); }}><DialogContent className="instructor-board-dialog"><DialogTitle>{t("instructor.board.importIssue")}</DialogTitle><DialogDescription>{t("instructor.board.issueIntakeHint")}</DialogDescription>
		{issues.isLoading ? <p>{t("instructor.loading")}</p> : null}{issues.error ? <p role="alert" className="text-destructive">{issues.error.message}</p> : null}
		<form className="flex flex-wrap items-end gap-2" onSubmit={(event) => { event.preventDefault(); const parsed = Number(lookup); if (Number.isSafeInteger(parsed) && parsed > 0) { setNumber(null); setName(""); setError(null); setPreviewNumber(parsed); } }}><label className="min-w-0 flex-1 text-sm">{t("instructor.board.issueNumber")}<Input type="number" required min={1} step={1} value={lookup} onChange={(event) => setLookup(event.target.value)} disabled={intake.isPending} className="mt-1" /></label><Button type="submit" variant="outline" disabled={intake.isPending || !Number.isSafeInteger(Number(lookup)) || Number(lookup) < 1}>{t("instructor.board.previewIssue")}</Button>{previewNumber ? <Button variant="ghost" type="button" disabled={intake.isPending} onClick={() => { setPreviewNumber(null); setNumber(null); setLookup(""); setName(""); }}>{t("instructor.board.showOpenIssues")}</Button> : null}</form>
		{issues.data ? <><p className="text-sm font-medium">{issues.data.repository}</p>{issues.data.truncated ? <p className="text-xs text-muted-foreground">{t("instructor.board.issueListTruncated")}</p> : null}<label className="block text-sm">{t("instructor.issue")}<select required aria-label={t("instructor.issue")} value={number ?? ""} disabled={intake.isPending} onChange={(event) => { setNumber(event.target.value ? Number(event.target.value) : null); setName(""); setError(null); }} className="mt-2 w-full rounded-md border border-input bg-background p-2"><option value="">{t("instructor.board.chooseIssue")}</option>{issues.data.issues.map((issue) => <option key={issue.number} value={issue.number}>#{issue.number} · {issue.title}{issue.linkedWorkId ? ` · ${t("instructor.board.issueLinked")}` : ""}</option>)}</select></label>{!issues.data.issues.length ? <p className="text-sm text-muted-foreground">{t("instructor.board.noIssues")}</p> : null}</> : null}
		{selected ? <section className="space-y-3 rounded-lg border border-border p-4"><h3 className="font-semibold">#{selected.number} · {selected.title}</h3><p className="text-xs text-muted-foreground">{t(selected.state.toLowerCase() === "open" ? "instructor.board.issueOpen" : "instructor.board.issueClosed")}</p><p className="text-xs text-muted-foreground">{selected.labels.join(" · ")}{selected.assignees.length ? ` · ${selected.assignees.join(", ")}` : ""}</p><p className="max-h-48 overflow-y-auto whitespace-pre-wrap break-words text-sm">{selected.body || t("instructor.board.noIssueDescription")}</p>{selected.bodyTruncated ? <p className="text-xs text-muted-foreground">{t("instructor.board.issueBodyTruncated")}</p> : null}<a href={selected.url} target="_blank" rel="noopener noreferrer" className="text-sm underline">{t("instructor.board.openIssue")}</a>{!selected.linkedWorkId ? <label className="block text-sm">{t("instructor.name")}<Input maxLength={120} value={name} onChange={(event) => setName(event.target.value)} placeholder={t("instructor.board.optionalWorkName")} disabled={intake.isPending} className="mt-1" /></label> : <p className="text-sm">{t("instructor.board.issueAlreadyLinked")}</p>}</section> : null}
		{error ? <p role="alert" className="text-destructive">{error}</p> : null}
		<Button disabled={!selected || intake.isPending || (selected.state.toLowerCase() !== "open" && !selected.linkedWorkId)} onClick={async () => { if (submitting.current) return; submitting.current = true; const capturedProject = projectId; setError(null); try { const work = await intake.mutateAsync(); if (activeProject.current === capturedProject) await onImported(work); } catch (cause) { if (activeProject.current === capturedProject) setError(cause instanceof Error ? cause.message : t("instructor.commandFailed")); } finally { submitting.current = false; } }}>{t(selected?.linkedWorkId ? "instructor.board.openLinkedWork" : "instructor.board.importSelectedIssue")}</Button>
	</DialogContent></Dialog>;
}
