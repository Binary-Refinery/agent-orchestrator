import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Play, Settings2 } from "lucide-react";
import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { useWorkspaceScope } from "../hooks/useWorkspaceQuery";
import { useProjectOrchestratorAction } from "../hooks/useProjectOrchestratorAction";
import { initialNativeRoleSelection } from "../lib/native-effort-defaults";
import { useUiStore } from "../stores/ui-store";
import { Button } from "./ui/button";

export function InstructorProjectHeader({ projectId, name, count, onIntake, onNewWork, canCreateWork }: { projectId: string; name: string; count: number; canCreateWork: boolean; onIntake: () => void; onNewWork: () => void }) {
	const { t } = useTranslation();
	const openSettings = useUiStore((state) => state.openProjectSettings);
	const scope = useWorkspaceScope(projectId).data;
	const actions = useProjectOrchestratorAction({ projectId, project: scope?.project, orchestrator: scope?.orchestrator, source: "board" });
	const project = useQuery({ queryKey: ["project", projectId], queryFn: async () => {
		const { data, error } = await apiClient.GET("/api/v1/projects/{id}", { params: { path: { id: projectId } } });
		if (error) throw new Error(apiErrorMessage(error));
		if (data?.status !== "ok" || !data.project) throw new Error(t("settings.project.degraded"));
		return data.project as components["schemas"]["Project"];
	}, retry: false });
	const config = project.data?.config;
	const harness = config?.orchestrator?.agent || project.data?.agent;
	const selection = initialNativeRoleSelection(harness ?? "", config?.agentConfig, config?.orchestrator?.agentConfig);
	const configured = [harness, selection?.model, selection?.effort].map((value) => value || t("instructor.unknown")).join(" · ");
	const orchestrator = scope?.orchestrator;
	const status = !scope ? "unknown" : !orchestrator ? "notStarted" : orchestrator.statusReadiness && orchestrator.statusReadiness !== "ready" ? "unknown" : orchestrator.isTerminated || orchestrator.activity?.state === "exited" ? "exited" : orchestrator.activity?.state === "active" ? "running" : orchestrator.activity?.state === "idle" ? "available" : orchestrator.activity?.state === "waiting_input" ? "waitingInput" : orchestrator.activity?.state === "blocked" ? "blocked" : "unknown";
	const initials = name.split(/\s+/).filter(Boolean).slice(0, 2).map((word) => [...word][0]).join("").toLocaleUpperCase();
	return <header className="instructor-board-project-head">
		<h2 className="instructor-board-project-name"><span className="instructor-board-project-symbol" aria-hidden="true">{initials}</span><span>{name}</span><span className="instructor-board-project-count">{t("instructor.board.workCount", { count })}</span></h2>
		<p className="instructor-board-orchestrator"><span className="instructor-board-dot" data-status={status} aria-hidden="true" />{t("instructor.board.projectOrchestrator")} · {t(`instructor.board.orchestrator.${status}`)}{orchestrator ? <span> · {orchestrator.provider}</span> : null}</p>
		<button className="instructor-board-model-link" aria-label={t("instructor.board.projectModelSettings", { name })} onClick={() => openSettings(projectId, "agents")} title={t("instructor.board.configuredModelHint")}><span>{configured}</span><span>{t("instructor.board.change")}</span><Settings2 className="size-3" aria-hidden="true" /></button>
		{project.error ? <p role="alert" className="instructor-board-project-error">{project.error.message}</p> : null}
		<div className="instructor-board-project-controls"><Button size="sm" variant="outline" disabled={actions.isSpawning || actions.isProjectRestarting || actions.isProvisioning || project.isLoading || Boolean(project.error) || !scope?.project} onClick={() => actions.openOrchestrator()}><Play className="size-3" aria-hidden="true" />{t(orchestrator ? "instructor.board.openOrchestrator" : "instructor.board.startProject")}</Button><Button size="sm" variant="ghost" onClick={onIntake}>{t("instructor.board.importIssue")}</Button>{canCreateWork ? <Button size="sm" variant="ghost" onClick={onNewWork}>{t("instructor.new")}</Button> : null}</div>
		{actions.spawnError ? <p role="alert" className="instructor-board-project-error">{actions.spawnError}</p> : null}
	</header>;
}
