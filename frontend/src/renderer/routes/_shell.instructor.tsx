import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_shell/instructor")({
	validateSearch: (search: Record<string, unknown>): { workId?: string; projectId?: string } => ({
		...(typeof search.workId === "string" ? { workId: search.workId } : {}),
		...(typeof search.projectId === "string" ? { projectId: search.projectId } : {}),
	}),
	beforeLoad: ({ search }) => { throw redirect({ to: "/instructor-board", search, replace: true }); },
});
