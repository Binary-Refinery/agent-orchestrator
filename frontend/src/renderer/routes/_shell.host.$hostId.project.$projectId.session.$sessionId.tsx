import { createFileRoute } from "@tanstack/react-router";
import { RemoteSessionView } from "../components/RemoteSessionView";

export const Route = createFileRoute("/_shell/host/$hostId/project/$projectId/session/$sessionId")({
	component: HostProjectSessionRoute,
});

function HostProjectSessionRoute() {
	const { hostId, sessionId } = Route.useParams();
	return <RemoteSessionView key={`${hostId}:${sessionId}`} hostId={hostId} sessionId={sessionId} />;
}
