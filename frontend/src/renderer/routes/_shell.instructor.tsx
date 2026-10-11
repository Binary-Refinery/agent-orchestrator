import { createFileRoute } from "@tanstack/react-router";
import { InstructorView } from "../components/InstructorView";

export const Route = createFileRoute("/_shell/instructor")({
	validateSearch: (search: Record<string, unknown>): { workId?: string } => typeof search.workId === "string" ? { workId: search.workId } : {},
	component: InstructorRoute,
});

function InstructorRoute() {
	const { workId } = Route.useSearch();
	return <InstructorView initialWorkId={workId} />;
}
