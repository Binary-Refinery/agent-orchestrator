import { useQuery } from "@tanstack/react-query";
import { ArrowUp, Loader2 } from "lucide-react";
import { useId, useState, useSyncExternalStore, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import type { components } from "../../api/schema";
import { apiErrorMessage } from "../lib/api-client";
import { clientForHost, connectedHosts, subscribeConnectedHosts } from "../lib/host-clients";
import { LOCAL_HOST } from "../lib/hosts";
import { STANDALONE_WORKSPACE_ID } from "../types/workspace";
import { Button } from "./ui/button";
import { Label } from "./ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

type SpawnRequest = components["schemas"]["SpawnSessionRequest"];

export function RemoteSpawnSession({ hostId, onCreated }: { hostId: string; onCreated: (sessionId: string) => void }) {
	const { t } = useTranslation();
	const projectFieldId = useId();
	const promptFieldId = useId();
	const connected = useSyncExternalStore(subscribeConnectedHosts, connectedHosts).includes(hostId) && hostId !== LOCAL_HOST;
	const [projectChoice, setProjectChoice] = useState<{ hostId: string; id: string }>();
	const [agentChoice, setAgentChoice] = useState<{ hostId: string; id: string }>();
	const [mode, setMode] = useState<"chat" | "tui">("chat");
	const [prompt, setPrompt] = useState("");
	const [submitting, setSubmitting] = useState(false);
	const [submitError, setSubmitError] = useState("");
	const projects = useQuery({
		queryKey: ["remote-spawn-projects", hostId],
		enabled: connected,
		queryFn: async () => {
			const { data, error } = await clientForHost(hostId).GET("/api/v1/projects");
			if (error) throw new Error(apiErrorMessage(error));
			return data?.projects ?? [];
		},
	});
	const agents = useQuery({
		queryKey: ["remote-spawn-agents", hostId],
		enabled: connected,
		queryFn: async () => {
			const { data, error } = await clientForHost(hostId).POST("/api/v1/agents/readiness/ensure", {
				body: { purpose: "display" },
			});
			if (error) throw new Error(apiErrorMessage(error));
			return data?.agents.filter((agent) => agent.effectiveReadiness === "ready") ?? [];
		},
	});
	const settings = useQuery({
		queryKey: ["remote-spawn-settings", hostId],
		enabled: connected,
		queryFn: async () => {
			const { data, error } = await clientForHost(hostId).GET("/api/v1/settings");
			if (error) throw new Error(apiErrorMessage(error));
			return data;
		},
	});
	const readyAgents = agents.data ?? [];
	const availableProjects = projects.data?.filter((project) => !project.folderMissing) ?? [];
	const projectId = projectChoice?.hostId === hostId ? projectChoice.id : "";
	const selectedAgent = (agentChoice?.hostId === hostId ? agentChoice.id : "") || readyAgents[0]?.id || "";
	const supportsChat = settings.data?.chatHarnesses?.includes(selectedAgent) ?? false;
	const selectedMode = supportsChat ? mode : "tui";
	const canSubmit = connected && projects.isSuccess && agents.isSuccess && settings.isSuccess && readyAgents.some((agent) => agent.id === selectedAgent) && !!prompt.trim() && !submitting;

	const submit = async (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		if (!canSubmit) return;
		setSubmitting(true);
		setSubmitError("");
		try {
			const task = prompt.trim();
			const { data, error } = await clientForHost(hostId).POST("/api/v1/sessions", {
				body: {
					kind: "worker",
					...(projectId ? { projectId } : {}),
					harness: selectedAgent as SpawnRequest["harness"],
					mode: selectedMode,
					prompt: task,
					displayName: task.slice(0, 100),
				},
			});
			if (error) throw new Error(apiErrorMessage(error, t("newTask.unableToStart")));
			if (!data?.session?.id) throw new Error(t("remote.noSessionId"));
			onCreated(data.session.id);
		} catch (error) {
			setSubmitError(error instanceof Error ? error.message : t("newTask.unableToStart"));
		} finally {
			setSubmitting(false);
		}
	};

	return <form aria-label={t("remote.startTaskAria")} className="flex flex-col gap-3 px-4 pb-4" onSubmit={(event) => void submit(event)}>
		<div className="flex flex-col gap-2">
			<Label htmlFor={projectFieldId}>{t("createProject.project")}</Label>
			<Select value={projectId || STANDALONE_WORKSPACE_ID} disabled={!connected || !projects.isSuccess || submitting} onValueChange={(id) => setProjectChoice({ hostId, id: id === STANDALONE_WORKSPACE_ID ? "" : id })}>
				<SelectTrigger id={projectFieldId} size="sm" className="w-full" aria-label={t("createProject.project")}>
					<SelectValue />
				</SelectTrigger>
				<SelectContent position="popper" align="start">
					<SelectItem value={STANDALONE_WORKSPACE_ID}>{t("remote.standalone")}</SelectItem>
					{availableProjects.map((project) => <SelectItem key={project.id} value={project.id}>{project.name}</SelectItem>)}
				</SelectContent>
			</Select>
		</div>
		{!connected && <p role="alert" className="text-sm text-destructive">{t("remote.connectBeforeStart")}</p>}
		{agents.isSuccess && !readyAgents.length && <p role="alert" className="text-sm text-muted-foreground">{t("remote.noReadyAgent")}</p>}
		<div className="composer-prompt-surface overflow-hidden rounded-lg border border-border bg-input/30">
			<Label htmlFor={promptFieldId} className="sr-only">{t("newTask.task")}</Label>
			<textarea id={promptFieldId} className="min-h-28 w-full resize-y bg-transparent px-4 pb-3 pt-4 text-md leading-relaxed text-foreground outline-none placeholder:text-passive disabled:opacity-50" placeholder={t("newTask.titlePlaceholder")} value={prompt} disabled={submitting} onChange={(event) => setPrompt(event.target.value)} />
			{(projects.isError || agents.isError || settings.isError) && <p role="alert" className="px-4 pb-2 text-caption text-destructive">{t("remote.loadSpawnOptionsFailed")}</p>}
			{submitError && <p role="alert" className="px-4 pb-2 text-caption text-destructive">{submitError}</p>}
			<div className="composer-toolbar">
				<div className="composer-run-controls" role="group" aria-label={t("newTask.runsWith")}>
					<div className="composer-toolbar-slot">
						<Select value={selectedAgent} disabled={!connected || !readyAgents.length || submitting} onValueChange={(id) => setAgentChoice({ hostId, id })}>
							<SelectTrigger size="sm" className="composer-chip composer-toolbar-option w-full justify-between" aria-label={t("newTask.agent")}>
								<SelectValue placeholder={t("newTask.selectAgent")} />
							</SelectTrigger>
							<SelectContent position="popper" align="start">
								{readyAgents.map((agent) => <SelectItem key={agent.id} value={agent.id}>{agent.label}</SelectItem>)}
							</SelectContent>
						</Select>
					</div>
					<div className="composer-toolbar-slot">
						<Select value={selectedMode} disabled={!settings.isSuccess || submitting} onValueChange={(value) => setMode(value as "chat" | "tui")}>
							<SelectTrigger size="sm" className="composer-chip composer-toolbar-option w-full justify-between" aria-label={t("remote.interface")}>
								<SelectValue />
							</SelectTrigger>
							<SelectContent position="popper" align="start">
								<SelectItem value="chat" disabled={!supportsChat}>{t("settings.sessionInterface.chat")}</SelectItem>
								<SelectItem value="tui">{t("settings.sessionInterface.terminal")}</SelectItem>
							</SelectContent>
						</Select>
					</div>
				</div>
				<Button type="submit" size="icon-sm" className="size-7 rounded-full bg-foreground text-background hover:bg-foreground/90" aria-label={submitting ? t("remote.starting") : t("remote.startTitle")} disabled={!canSubmit}>
					{submitting ? <Loader2 className="size-3.5 animate-spin" aria-hidden="true" /> : <ArrowUp className="size-3.5" aria-hidden="true" />}
				</Button>
			</div>
		</div>
	</form>;
}
