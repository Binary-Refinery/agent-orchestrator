import { describe, expect, it } from "vitest";
import { sessionNavigateTarget } from "./navigate-to-session";

describe("sessionNavigateTarget", () => {
	it("keeps local URLs and identifies the remote host in a session URL", () => {
		expect(sessionNavigateTarget("project-1", "session-1")).toEqual({
			to: "/projects/$projectId/sessions/$sessionId",
			params: { projectId: "project-1", sessionId: "session-1" },
		});
		expect(sessionNavigateTarget("project-1", "session-1", "machine-a")).toEqual({
			to: "/host/$hostId/project/$projectId/session/$sessionId",
			params: { hostId: "machine-a", projectId: "project-1", sessionId: "session-1" },
		});
	});
});
