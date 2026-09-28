import { apiErrorCode, apiErrorMessage } from "./api-client";
import { clientForHost } from "./host-clients";
import { refKey } from "./hosts";
import { sessionAgentExited, type WorkspaceSession } from "../types/workspace";

const inFlight = new Map<string, Promise<string>>();

/** Open one project's orchestrator on its owning daemon. Shared by the board
 * and sidebar so two rapid clicks cannot launch two copies. */
export function openRemoteOrchestrator(
	hostId: string,
	projectId: string,
	orchestrator?: WorkspaceSession,
	mode?: "tui",
	clean = false,
): Promise<string> {
	const key = `${refKey({ host: hostId, id: projectId })}:${clean ? "clean" : "ensure"}`;
	const current = inFlight.get(key);
	if (current) return current;
	if (!clean && orchestrator && !sessionAgentExited(orchestrator)) return Promise.resolve(orchestrator.id);
	const request = (async () => {
		const client = clientForHost(hostId);
		if (!clean && orchestrator) {
			const { error } = await client.POST("/api/v1/sessions/{sessionId}/resume-agent", {
				params: { path: { sessionId: orchestrator.id } },
			});
			if (error && apiErrorCode(error) !== "AGENT_NOT_EXITED") throw new Error(apiErrorMessage(error));
			return orchestrator.id;
		}
		const { data, error } = await client.POST("/api/v1/orchestrators", {
			body: { projectId, ...(mode ? { mode } : {}), ...(clean ? { clean: true } : {}) },
		});
		if (error) throw Object.assign(new Error(apiErrorMessage(error)), { code: apiErrorCode(error) });
		if (!data?.orchestrator?.id) throw new Error("Could not spawn orchestrator");
		return data.orchestrator.id;
	})();
	inFlight.set(key, request);
	void request.finally(() => {
		if (inFlight.get(key) === request) inFlight.delete(key);
	}).catch(() => undefined);
	return request;
}
