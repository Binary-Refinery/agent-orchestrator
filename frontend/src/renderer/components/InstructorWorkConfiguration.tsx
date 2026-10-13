import { useTranslation } from "react-i18next";
import type { InstructorWork } from "../hooks/useInstructor";
import { useInstructorEffectivePolicy } from "../hooks/useInstructorRecovery";
import { useUiStore } from "../stores/ui-store";
import { Button } from "./ui/button";

export function InstructorWorkConfiguration({ work, onRebind }: { work: InstructorWork; onRebind?: () => void }) {
	const { t } = useTranslation();
	const effective = useInstructorEffectivePolicy(work.projectId);
	const openSettings = useUiStore((state) => state.openProjectSettings);
	const differs = effective.data && effective.data.policy.version !== work.policyVersion;
	return <section className="space-y-2 rounded-md border border-border p-3">
		<h3 className="font-medium">{t("instructor.configuration.binding")}</h3>
		<p className="break-all text-xs">{t("instructor.configuration.pinned")}: {work.policyVersion}</p>
		{effective.isLoading ? <p className="text-xs">{t("instructor.loading")}</p> : null}
		{effective.error ? <p role="alert" className="text-sm text-destructive">{effective.error.message}</p> : null}
		{effective.data ? <><p className="break-all text-xs">{t("instructor.configuration.available")}: {effective.data.policy.version}</p><p className="text-xs text-muted-foreground">{t("instructor.configuration.source")}: {effective.data.source}</p><p className="text-sm">{t(differs ? "instructor.configuration.differs" : "instructor.configuration.current")}</p></> : null}
		<div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" onClick={() => openSettings(work.projectId, "agents")}>{t("instructor.recovery.openSettings")}</Button>{onRebind ? <Button size="sm" variant="outline" disabled={!effective.data || Boolean(effective.error) || !differs} onClick={onRebind}>{t("instructor.rebind.title")}</Button> : null}</div>
	</section>;
}
