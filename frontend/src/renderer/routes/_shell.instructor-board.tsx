import { createFileRoute } from "@tanstack/react-router";
import { InstructorBoard } from "../components/InstructorBoard";

export const Route = createFileRoute("/_shell/instructor-board")({
	validateSearch: (search: Record<string, unknown>): { workId?: string; projectId?: string } => ({
		...(typeof search.workId === "string" ? { workId: search.workId } : {}),
		...(typeof search.projectId === "string" ? { projectId: search.projectId } : {}),
	}),
	component: InstructorBoardRoute,
});

function InstructorBoardRoute() {
	const { workId, projectId } = Route.useSearch();
	return <InstructorBoard initialWorkId={workId} initialProjectId={projectId} />;
}
