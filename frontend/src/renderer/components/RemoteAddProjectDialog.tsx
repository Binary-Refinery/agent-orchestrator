import * as Dialog from "@radix-ui/react-dialog";
import { useQuery } from "@tanstack/react-query";
import { ArrowUp, ChevronRight, Folder, FolderGit2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import type { components } from "../../api/schema";
import { apiErrorMessage } from "../lib/api-client";
import { clientForHost } from "../lib/host-clients";
import { Button } from "./ui/button";
import { Checkbox } from "./ui/checkbox";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";
import { Skeleton } from "./ui/skeleton";

type Source = "existing" | "clone";

export function RemoteAddProjectDialog({ hostId, hostLabel, connected, onCreated, onOpenChange }: {
	hostId: string;
	hostLabel: string;
	connected: boolean;
	onCreated: (projectId: string, ready: boolean) => void;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation();
	const [source, setSource] = useState<Source>("existing");
	const [path, setPath] = useState("");
	const [initializeGit, setInitializeGit] = useState(false);
	const [initializedPath, setInitializedPath] = useState("");
	const [remoteUrl, setRemoteUrl] = useState("");
	const [destinationParent, setDestinationParent] = useState("");
	const [submitting, setSubmitting] = useState(false);
	const [error, setError] = useState("");
	const [createdProjectId, setCreatedProjectId] = useState("");
	const [workerChoice, setWorkerChoice] = useState("");
	const [orchestratorChoice, setOrchestratorChoice] = useState("");
	const [browseFor, setBrowseFor] = useState<"path" | "destinationParent" | null>(null);
	const [browsePath, setBrowsePath] = useState("");
	const agents = useQuery({
		queryKey: ["remote-project-agents", hostId],
		enabled: connected,
		queryFn: async () => {
			const { data, error } = await clientForHost(hostId).POST("/api/v1/agents/readiness/ensure", { body: { purpose: "display" } });
			if (error) throw new Error(apiErrorMessage(error));
			return data?.agents.filter((agent) => agent.effectiveReadiness === "ready") ?? [];
		},
	});
	const readyAgents = agents.data ?? [];
	const workerAgent = workerChoice || readyAgents[0]?.id || "";
	const orchestratorAgent = orchestratorChoice || readyAgents[0]?.id || "";
	const directory = useQuery({
		queryKey: ["remote-fs-dirs", hostId, browsePath],
		enabled: connected && browseFor !== null,
		retry: false,
		queryFn: async () => {
			const response = await clientForHost(hostId).GET("/api/v1/fs/dirs", { params: { query: browsePath ? { path: browsePath } : {} } });
			if (response.error) throw new Error(apiErrorMessage(response.error));
			if (!response.data) throw new Error("Could not list folders on this host.");
			return response.data;
		},
	});
	const browse = (field: "path" | "destinationParent") => {
		setBrowsePath((field === "path" ? path : destinationParent).trim());
		setBrowseFor(field);
	};
	const canSubmit = connected && !submitting && (createdProjectId !== "" || (
		agents.isSuccess && readyAgents.some((agent) => agent.id === workerAgent) && readyAgents.some((agent) => agent.id === orchestratorAgent) &&
		(source === "existing" ? !!path.trim() : !!remoteUrl.trim() && !!destinationParent.trim())
	));

	const submit = async (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		if (!canSubmit) return;
		setSubmitting(true);
		setError("");
		let projectId = createdProjectId;
		let initializing = false;
		try {
			const client = clientForHost(hostId);
			if (!projectId) {
				const projectPath = path.trim();
				if (source === "existing" && initializeGit && initializedPath !== projectPath) {
					initializing = true;
					const setup = await client.POST("/api/v1/projects/initialize", { body: { path: projectPath } });
					if (setup.error) throw new Error(apiErrorMessage(setup.error));
					setInitializedPath(projectPath);
					initializing = false;
				}
				const config: components["schemas"]["ProjectConfig"] = {
					worker: { agent: workerAgent }, orchestrator: { agent: orchestratorAgent },
				};
				const response = source === "existing"
					? await client.POST("/api/v1/projects", { body: { path: projectPath, config } })
					: await client.POST("/api/v1/projects/clone", { body: { remoteUrl: remoteUrl.trim(), destinationParent: destinationParent.trim(), config } });
				if (response.error) throw new Error(apiErrorMessage(response.error));
				if (!response.data?.project) throw new Error(t("createProject.couldNotAdd"));
				projectId = response.data.project.id;
				setCreatedProjectId(projectId);
				onCreated(projectId, false);
			}
			const spawn = await client.POST("/api/v1/orchestrators", { body: { projectId } });
			if (spawn.error) throw new Error(apiErrorMessage(spawn.error));
			if (!spawn.data?.orchestrator?.id) throw new Error("Could not start orchestrator on this host.");
			onCreated(projectId, true);
			onOpenChange(false);
		} catch (cause) {
			const message = cause instanceof Error ? cause.message : t("createProject.couldNotAdd");
			setError(projectId ? `Project added, but could not start orchestrator: ${message}` : initializing ? `Could not initialize Git repository on ${hostLabel}: ${message}` : message);
		} finally {
			setSubmitting(false);
		}
	};

	return <Dialog.Root open onOpenChange={(open) => { if (!submitting) onOpenChange(open); }}>
		<Dialog.Portal>
			<Dialog.Overlay className="dialog-overlay data-[state=open]:animate-overlay-in data-[state=closed]:animate-overlay-out" />
			<Dialog.Content className="fixed left-1/2 top-1/2 z-overlay max-h-[calc(100dvh-24px)] w-dialog-xl -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-lg border border-border bg-popover p-4 text-popover-foreground shadow-xl data-[state=open]:animate-modal-in data-[state=closed]:animate-modal-out motion-reduce:animate-none">
				<Dialog.Title className="settings-dialog-title text-balance">{t("remote.addProjectOn", { label: hostLabel, defaultValue: "Add project on {{label}}" })}</Dialog.Title>
				<Dialog.Description className="mt-1 text-pretty text-sm text-muted-foreground">
					{t("remote.projectPathHint", { label: hostLabel, defaultValue: "Use a repository folder on {{label}}, or clone one there from Git." })}
				</Dialog.Description>
				<form className="mt-5 space-y-4" onSubmit={(event) => void submit(event)}>
					<div className="space-y-2">
						<Label htmlFor="remote-project-source">{t("createProject.projectSource", { defaultValue: "Project source" })}</Label>
						<Select value={source} disabled={submitting || !!createdProjectId} onValueChange={(value) => { setSource(value as Source); setBrowseFor(null); setError(""); }}>
							<SelectTrigger id="remote-project-source" className="w-full"><SelectValue /></SelectTrigger>
							<SelectContent>
								<SelectItem value="existing">{t("remote.existingFolder", { defaultValue: "Existing folder on host" })}</SelectItem>
								<SelectItem value="clone">{t("createProject.cloneFromGit")}</SelectItem>
							</SelectContent>
						</Select>
					</div>
					{source === "existing" ? <div className="space-y-2">
						<Label htmlFor="remote-project-path">{t("remote.projectPath", { defaultValue: "Repository path on host" })}</Label>
						<div className="flex gap-2">
							<Input id="remote-project-path" value={path} onChange={(event) => setPath(event.target.value)} disabled={submitting || !!createdProjectId} placeholder="/home/you/code/project" autoFocus />
							<Button type="button" variant="outline" disabled={!connected || submitting || !!createdProjectId} onClick={() => browse("path")}>{t("remote.browseFolders", { defaultValue: "Browse" })}</Button>
						</div>
						<Label htmlFor="remote-initialize-git" className="flex cursor-pointer items-start gap-2 pt-2 text-sm">
							<Checkbox id="remote-initialize-git" aria-label="Initialize Git repository and create initial commit" aria-describedby="remote-initialize-git-help" checked={initializeGit} disabled={submitting || !!createdProjectId} onCheckedChange={(checked) => setInitializeGit(checked === true)} />
							<span><span className="block">Initialize Git repository and create initial commit</span><span id="remote-initialize-git-help" className="block text-xs text-muted-foreground">Use for a plain folder or repository without commits on {hostLabel}.</span></span>
						</Label>
					</div> : <>
						<div className="space-y-2">
							<Label htmlFor="remote-project-url">{t("createProject.cloneRepositoryUrl", { defaultValue: "Git repository URL" })}</Label>
							<Input id="remote-project-url" value={remoteUrl} onChange={(event) => setRemoteUrl(event.target.value)} disabled={submitting || !!createdProjectId} placeholder="https://github.com/owner/repo.git" autoFocus />
						</div>
						<div className="space-y-2">
							<Label htmlFor="remote-project-destination">{t("remote.cloneDestination", { defaultValue: "Parent folder on host" })}</Label>
							<div className="flex gap-2">
								<Input id="remote-project-destination" value={destinationParent} onChange={(event) => setDestinationParent(event.target.value)} disabled={submitting || !!createdProjectId} placeholder="/home/you/code" />
								<Button type="button" variant="outline" disabled={!connected || submitting || !!createdProjectId} onClick={() => browse("destinationParent")}>{t("remote.browseFolders", { defaultValue: "Browse" })}</Button>
							</div>
						</div>
					</>}
					<div className="space-y-2">
						<Label htmlFor="remote-worker-agent">{t("createProject.workerAgent")}</Label>
						<Select value={workerAgent} disabled={!connected || !readyAgents.length || submitting || !!createdProjectId} onValueChange={setWorkerChoice}>
							<SelectTrigger id="remote-worker-agent" className="w-full"><SelectValue placeholder={t("createProject.selectWorker")} /></SelectTrigger>
							<SelectContent>{readyAgents.map((agent) => <SelectItem key={agent.id} value={agent.id}>{agent.label}</SelectItem>)}</SelectContent>
						</Select>
					</div>
					<div className="space-y-2">
						<Label htmlFor="remote-orchestrator-agent">{t("createProject.orchestratorAgent")}</Label>
						<Select value={orchestratorAgent} disabled={!connected || !readyAgents.length || submitting || !!createdProjectId} onValueChange={setOrchestratorChoice}>
							<SelectTrigger id="remote-orchestrator-agent" className="w-full"><SelectValue placeholder={t("createProject.selectOrchestrator")} /></SelectTrigger>
							<SelectContent>{readyAgents.map((agent) => <SelectItem key={agent.id} value={agent.id}>{agent.label}</SelectItem>)}</SelectContent>
						</Select>
					</div>
					{agents.isPending && connected && <p role="status" className="text-sm text-muted-foreground">Loading agents on {hostLabel}…</p>}
					{agents.isError && <div className="flex items-center gap-2"><p role="alert" className="text-sm text-destructive">Could not load agents on this host.</p><Button type="button" variant="outline" size="sm" onClick={() => void agents.refetch()}>Try again</Button></div>}
					{agents.isSuccess && !readyAgents.length && <p role="alert" className="text-sm text-muted-foreground">No ready agents on this host.</p>}
					{browseFor && <div role="region" aria-label={`Folders on ${hostLabel}`} className="overflow-hidden rounded-md border border-border bg-background">
						<div className="flex items-center gap-2 border-b border-border px-3 py-2">
							<Button type="button" variant="ghost" size="icon-sm" aria-label="Parent folder" disabled={!connected || !directory.data || directory.data.parent === directory.data.path || directory.isFetching} onClick={() => directory.data && setBrowsePath(directory.data.parent)}>
								<ArrowUp className="size-4" aria-hidden="true" />
							</Button>
							<span className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground" title={directory.data?.path}>{directory.data?.path ?? "Folders on host"}</span>
							<Button type="button" variant="outline" size="sm" disabled={!connected || !directory.data || directory.isFetching} onClick={() => {
								if (!directory.data) return;
								if (browseFor === "path") setPath(directory.data.path);
								else setDestinationParent(directory.data.path);
								setBrowseFor(null);
							}}>Use this folder</Button>
						</div>
						<div className="max-h-48 overflow-y-auto p-1">
							{directory.isPending ? <div role="status" className="space-y-2 p-2" aria-label="Loading folders"><Skeleton className="h-7 w-full" /><Skeleton className="h-7 w-4/5" /></div> : directory.error ? <div className="flex items-center justify-between gap-2 p-3"><p role="alert" className="text-sm text-destructive">{directory.error.message}</p><Button type="button" variant="outline" size="sm" onClick={() => void directory.refetch()}>Try again</Button></div> : directory.data?.entries.length ? directory.data.entries.map((entry) => <button key={entry.path} type="button" disabled={!connected || directory.isFetching} className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm hover:bg-muted focus-visible:outline-2 focus-visible:outline-primary disabled:opacity-50" onClick={() => setBrowsePath(entry.path)}>
								{entry.gitRepo ? <FolderGit2 className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" /> : <Folder className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />}
								<span className="min-w-0 flex-1 truncate">{entry.name}</span><ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
							</button>) : <p className="p-3 text-sm text-muted-foreground">No subfolders here.</p>}
							{directory.data?.truncated && <p className="px-2 py-1 text-xs text-muted-foreground">Showing the first 500 folders.</p>}
						</div>
					</div>}
					{!connected && <p role="alert" className="text-sm text-destructive">{t("remote.connectBeforeStart")}</p>}
					{error && <p role="alert" className="text-sm text-destructive">{error}</p>}
					<div className="flex justify-end gap-2">
						<Button type="button" variant="outline" disabled={submitting} onClick={() => onOpenChange(false)}>{t("createProject.cancel")}</Button>
						<Button type="submit" disabled={!canSubmit}>{submitting ? t("createProject.creating") : createdProjectId ? "Retry orchestrator" : t("createProject.addCodeTitle")}</Button>
					</div>
				</form>
			</Dialog.Content>
		</Dialog.Portal>
	</Dialog.Root>;
}
