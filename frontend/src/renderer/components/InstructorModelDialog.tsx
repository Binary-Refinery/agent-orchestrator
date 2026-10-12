import { useCallback, useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { instructorQueryKey, useInstructorPolicy, useSaveInstructorPolicy, type InstructorPolicy } from "../hooks/useInstructor";
import { InstructorRoleEditor } from "./InstructorPolicyDialog";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./ui/dialog";

// A role edit preserves all workflow/profile rules and the other role defaults.
// The daemon validates the native selection and policy version on save.
export function InstructorModelDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
	const { t } = useTranslation();
	const client = useQueryClient();
	const policy = useInstructorPolicy(open);
	const save = useSaveInstructorPolicy();
	const [draft, setDraft] = useState<InstructorPolicy | null>(null);
	const [valid, setValid] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const validity = useCallback((next: boolean) => setValid(next), []);
	useEffect(() => {
		if (open && policy.data) { setDraft(structuredClone(policy.data)); setError(null); }
	}, [open, policy.data]);

	async function submit() {
		if (!draft || !valid) return;
		setError(null);
		try {
			const result = await save.mutateAsync(draft);
			client.setQueryData([...instructorQueryKey, "policy"], result);
			onClose();
		} catch (cause) { setError(cause instanceof Error ? cause.message : t("instructor.saveFailed")); }
	}
	const settings = draft?.roles?.instructor;
	return <Dialog open={open} onOpenChange={(next) => { if (!next && !save.isPending) onClose(); }}>
		<DialogContent className="instructor-board-dialog">
			<DialogTitle>{t("instructor.board.instructorModel")}</DialogTitle>
			<DialogDescription>{t("instructor.board.modelHint")}</DialogDescription>
			{policy.isLoading ? <p>{t("instructor.loading")}</p> : null}
			{policy.error ? <p role="alert" className="text-destructive">{policy.error.message}</p> : null}
			{draft && settings ? <InstructorRoleEditor role="instructor" settings={settings} disabled={save.isPending} onValidityChange={validity} onChange={(next) => setDraft({ ...draft, roles: { ...draft.roles, instructor: next } })} /> : null}
			{draft && !settings ? <p role="alert">{t("instructor.board.missingRole")}</p> : null}
			{error ? <p role="alert" className="text-destructive">{error}</p> : null}
			<Button disabled={save.isPending || !valid || !settings} onClick={() => void submit()}>{t("instructor.board.saveModel")}</Button>
		</DialogContent>
	</Dialog>;
}
