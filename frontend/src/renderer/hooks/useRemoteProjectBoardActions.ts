import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { apiErrorCode, apiErrorMessage } from "../lib/api-client";
import { refKey } from "../lib/hosts";
import { useShellMaybe } from "../lib/shell-context";
import { openRemoteOrchestrator } from "../lib/remote-orchestrator";
import { isChatPreflightCode } from "../lib/spawn-orchestrator";
import { useUiStore } from "../stores/ui-store";
import { hasConfiguredOrchestratorAgent, type WorkspaceSession, type WorkspaceSummary } from "../types/workspace";
import { remoteWorkspaceQueryKey } from "./useWorkspaceQuery";

export function useRemoteProjectBoardActions({ hostId, projectId, project, orchestrator, connected }: {
	hostId?: string;
	projectId?: string;
	project?: WorkspaceSummary;
	orchestrator?: WorkspaceSession;
	connected: boolean;
}) {
	const { t } = useTranslation();
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const shell = useShellMaybe();
	const requestNewTask = useUiStore((state) => state.requestNewTask);
	const [spawnError, setSpawnError] = useState("");
	const [spawnErrorCode, setSpawnErrorCode] = useState<string>();
	const [isSpawning, setSpawning] = useState(false);
	const [isProjectRestarting, setRestarting] = useState(false);
	const launching = useRef(false);
	const routeKey = hostId && projectId ? refKey({ host: hostId, id: projectId }) : null;
	const activeRoute = useRef<string | null>(routeKey);
	activeRoute.current = routeKey;
	useEffect(() => {
		activeRoute.current = routeKey;
		return () => { activeRoute.current = null; };
	}, [routeKey]);

	const launch = async (mode?: "tui", clean = false) => {
		if (!connected || !hostId || !projectId || !project || launching.current) return;
		setSpawnError("");
		setSpawnErrorCode(undefined);
		if (!orchestrator && !hasConfiguredOrchestratorAgent(project)) {
			shell?.openRemoteProjectSettings(hostId, projectId);
			return;
		}
		launching.current = true;
		setSpawning(true);
		setRestarting(clean);
		try {
			const sessionId = await openRemoteOrchestrator(hostId, projectId, orchestrator, mode, clean);
			await queryClient.invalidateQueries({ queryKey: remoteWorkspaceQueryKey(hostId) });
			if (activeRoute.current === routeKey) void navigate({
				to: "/host/$hostId/project/$projectId/session/$sessionId",
				params: { hostId, projectId, sessionId },
			});
		} catch (error) {
			if (activeRoute.current === routeKey) {
				setSpawnErrorCode(apiErrorCode(error));
				setSpawnError(error instanceof Error ? error.message : apiErrorMessage(error, t("shell.couldNotSpawn")));
			}
		} finally {
			launching.current = false;
			if (activeRoute.current === routeKey) {
				setSpawning(false);
				setRestarting(false);
			}
		}
	};

	return {
		orchestrator,
		isSpawning,
		isProjectRestarting,
		isProvisioning: false,
		spawnError,
		canCreateAsTui: isChatPreflightCode(spawnErrorCode),
		openNewTask: () => { if (hostId && projectId && connected) requestNewTask(projectId, hostId); },
		openOrchestrator: (mode?: "tui") => { void launch(mode); },
		restartOrchestrator: () => launch(undefined, true),
	};
}
