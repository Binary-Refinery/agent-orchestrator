import { useQuery } from "@tanstack/react-query";
import { useState, useSyncExternalStore, type FormEvent } from "react";
import type { components } from "../../api/schema";
import { apiErrorMessage } from "../lib/api-client";
import { clientForHost, connectedHosts, subscribeConnectedHosts } from "../lib/host-clients";
import { LOCAL_HOST } from "../lib/hosts";

type SpawnRequest = components["schemas"]["SpawnSessionRequest"];

export function RemoteSpawnSession({ hostId, onCreated }: { hostId: string; onCreated: (sessionId: string) => void }) {
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
	const readyAgents = agents.data ?? [];
	const projectId = projectChoice?.hostId === hostId ? projectChoice.id : "";
	const selectedAgent = (agentChoice?.hostId === hostId ? agentChoice.id : "") || readyAgents[0]?.id || "";
	const canSubmit = connected && projects.isSuccess && agents.isSuccess && readyAgents.some((agent) => agent.id === selectedAgent) && !!prompt.trim() && !submitting;

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
					mode,
					prompt: task,
					displayName: task.slice(0, 100),
				},
			});
			if (error) throw new Error(apiErrorMessage(error, "Could not start task"));
			if (!data?.session?.id) throw new Error("Remote host did not return a session ID");
			onCreated(data.session.id);
		} catch (error) {
			setSubmitError(error instanceof Error ? error.message : "Could not start task");
		} finally {
			setSubmitting(false);
		}
	};

	return <form aria-label="Start remote task" className="flex flex-col gap-4" onSubmit={(event) => void submit(event)}>
		{!connected && <p role="alert" className="text-sm text-destructive">Connect this remote host before starting a task.</p>}
		<label className="flex flex-col gap-1 text-sm">Project
			<select className="rounded-md border bg-background px-3 py-2" value={projectId} disabled={!connected || !projects.isSuccess} onChange={(event) => setProjectChoice({ hostId, id: event.target.value })}>
				<option value="">Standalone</option>
				{projects.data?.filter((project) => !project.folderMissing).map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}
			</select>
		</label>
		<label className="flex flex-col gap-1 text-sm">Agent
			<select className="rounded-md border bg-background px-3 py-2" value={selectedAgent} disabled={!connected || !readyAgents.length} onChange={(event) => setAgentChoice({ hostId, id: event.target.value })}>
				{readyAgents.map((agent) => <option key={agent.id} value={agent.id}>{agent.label}</option>)}
			</select>
		</label>
		{agents.isSuccess && !readyAgents.length && <p role="alert" className="text-sm text-muted-foreground">No agent is ready on this host. Configure one there first.</p>}
		<label className="flex flex-col gap-1 text-sm">Interface
			<select className="rounded-md border bg-background px-3 py-2" value={mode} onChange={(event) => setMode(event.target.value as "chat" | "tui")}>
				<option value="chat">Chat</option>
				<option value="tui">Terminal</option>
			</select>
		</label>
		<label className="flex flex-col gap-1 text-sm">Task
			<textarea className="min-h-24 rounded-md border bg-background px-3 py-2" value={prompt} onChange={(event) => setPrompt(event.target.value)} />
		</label>
		{(projects.isError || agents.isError) && <p role="alert" className="text-sm text-destructive">Could not load projects or agents from this host.</p>}
		{submitError && <p role="alert" className="text-sm text-destructive">{submitError}</p>}
		<button type="submit" className="self-start rounded-md border px-4 py-2 text-sm" disabled={!canSubmit}>{submitting ? "Starting…" : "Start on remote host"}</button>
	</form>;
}
