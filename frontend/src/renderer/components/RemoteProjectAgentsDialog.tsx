import * as Dialog from "@radix-ui/react-dialog";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import type { components } from "../../api/schema";
import { remoteWorkspaceQueryKey } from "../hooks/useWorkspaceQuery";
import { apiErrorMessage } from "../lib/api-client";
import { clientForHost } from "../lib/host-clients";
import { Button } from "./ui/button";
import { Label } from "./ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

type Project = components["schemas"]["Project"];

export function RemoteProjectAgentsDialog({ hostId, projectId, hostLabel, connected, onSaved, onOpenChange }: {
	hostId: string;
	projectId: string;
	hostLabel: string;
	connected: boolean;
	onSaved: () => void;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const workerId = useId();
	const orchestratorId = useId();
	const [workerChoice, setWorkerChoice] = useState("");
	const [orchestratorChoice, setOrchestratorChoice] = useState("");
	const [saving, setSaving] = useState(false);
	const [configSaved, setConfigSaved] = useState(false);
	const [error, setError] = useState("");
	const project = useQuery({
		queryKey: ["remote-project-settings", hostId, projectId],
		enabled: connected,
		queryFn: async (): Promise<Project> => {
			const { data, error: apiError } = await clientForHost(hostId).GET("/api/v1/projects/{id}", { params: { path: { id: projectId } } });
			if (apiError) throw new Error(apiErrorMessage(apiError));
			if (data?.status !== "ok") throw new Error(t("settings.project.degraded"));
			return data.project as Project;
		},
	});
	const agents = useQuery({
		queryKey: ["remote-project-agents", hostId],
		enabled: connected,
		queryFn: async () => {
			const { data, error: apiError } = await clientForHost(hostId).POST("/api/v1/agents/readiness/ensure", { body: { purpose: "display" } });
			if (apiError) throw new Error(apiErrorMessage(apiError));
			return data?.agents.filter((agent) => agent.effectiveReadiness === "ready") ?? [];
		},
	});
	const readyAgents = agents.data ?? [];
	const workerAgent = workerChoice || project.data?.config?.worker?.agent || readyAgents[0]?.id || "";
	const orchestratorAgent = orchestratorChoice || project.data?.config?.orchestrator?.agent || readyAgents[0]?.id || "";
	const canSave = connected && !saving && !!project.data && agents.isSuccess &&
		readyAgents.some((agent) => agent.id === workerAgent) && readyAgents.some((agent) => agent.id === orchestratorAgent);

	const save = async (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		if (!canSave || !project.data) return;
		setSaving(true);
		setError("");
		let saved = configSaved;
		try {
			const client = clientForHost(hostId);
			if (!configSaved) {
				const config: components["schemas"]["ProjectConfig"] = {
					...project.data.config,
					worker: { ...project.data.config?.worker, agent: workerAgent },
					orchestrator: { ...project.data.config?.orchestrator, agent: orchestratorAgent },
				};
				const result = await client.PUT("/api/v1/projects/{id}/config", {
					params: { path: { id: projectId } },
					body: { config },
				});
				if (result.error) throw new Error(apiErrorMessage(result.error));
				saved = true;
				setConfigSaved(true);
				await queryClient.invalidateQueries({ queryKey: remoteWorkspaceQueryKey(hostId) });
				onSaved();
			}
			const replacing = project.data.config?.orchestrator?.agent !== orchestratorAgent;
			const spawn = await client.POST("/api/v1/orchestrators", { body: { projectId, ...(replacing ? { clean: true } : {}) } });
			if (spawn.error) throw new Error(apiErrorMessage(spawn.error));
			await queryClient.invalidateQueries({ queryKey: remoteWorkspaceQueryKey(hostId) });
			onSaved();
			onOpenChange(false);
		} catch (cause) {
			const message = cause instanceof Error ? cause.message : t("settings.project.saveFailed");
			setError(saved ? `Agents saved, but the orchestrator could not start: ${message}` : message);
		} finally {
			setSaving(false);
		}
	};

	return <Dialog.Root open onOpenChange={(open) => { if (!saving) onOpenChange(open); }}>
		<Dialog.Portal>
			<Dialog.Overlay className="dialog-overlay data-[state=open]:animate-overlay-in data-[state=closed]:animate-overlay-out" />
			<Dialog.Content className="fixed left-1/2 top-1/2 z-overlay w-dialog-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-popover p-4 text-popover-foreground shadow-xl data-[state=open]:animate-modal-in data-[state=closed]:animate-modal-out motion-reduce:animate-none">
				<Dialog.Title className="settings-dialog-title text-balance">{t("settings.project.agents")} · {project.data?.name ?? hostLabel}</Dialog.Title>
				<Dialog.Description className="mt-1 text-pretty text-sm text-muted-foreground">{t("remote.agentsOnHost", { label: hostLabel, defaultValue: "Agents run on {{label}}. Changing the orchestrator agent replaces its current session." })}</Dialog.Description>
				<form className="mt-5 space-y-4" onSubmit={(event) => void save(event)}>
					{project.isPending || agents.isPending ? <p role="status" className="text-sm text-muted-foreground">{t("settings.project.loading")}</p> : null}
					{project.isError || agents.isError ? <p role="alert" className="text-sm text-destructive">{project.error?.message ?? agents.error?.message ?? t("settings.project.loadFailed")}</p> : null}
					{agents.isSuccess && readyAgents.length === 0 ? <p role="alert" className="text-sm text-destructive">{t("remote.noReadyAgent")}</p> : null}
					{project.data && readyAgents.length > 0 ? <>
						<div className="space-y-2">
							<Label htmlFor={workerId}>{t("createProject.workerAgent")}</Label>
							<Select value={workerAgent} disabled={!connected || saving || configSaved} onValueChange={setWorkerChoice}>
								<SelectTrigger id={workerId} className="w-full"><SelectValue /></SelectTrigger>
								<SelectContent>{readyAgents.map((agent) => <SelectItem key={agent.id} value={agent.id}>{agent.label}</SelectItem>)}</SelectContent>
							</Select>
						</div>
						<div className="space-y-2">
							<Label htmlFor={orchestratorId}>{t("createProject.orchestratorAgent")}</Label>
							<Select value={orchestratorAgent} disabled={!connected || saving || configSaved} onValueChange={setOrchestratorChoice}>
								<SelectTrigger id={orchestratorId} className="w-full"><SelectValue /></SelectTrigger>
								<SelectContent>{readyAgents.map((agent) => <SelectItem key={agent.id} value={agent.id}>{agent.label}</SelectItem>)}</SelectContent>
							</Select>
						</div>
					</> : null}
					{!connected ? <p role="alert" className="text-sm text-destructive">{t("remote.hostOffline")}</p> : null}
					{error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
					<div className="flex justify-end gap-2">
						<Button type="button" variant="outline" disabled={saving} onClick={() => onOpenChange(false)}>{t("createProject.cancel")}</Button>
						<Button type="submit" disabled={!canSave}>{saving ? t("createProject.creating") : configSaved ? t("createProject.retry") : t("settings.project.saveChanges")}</Button>
					</div>
				</form>
			</Dialog.Content>
		</Dialog.Portal>
	</Dialog.Root>;
}
