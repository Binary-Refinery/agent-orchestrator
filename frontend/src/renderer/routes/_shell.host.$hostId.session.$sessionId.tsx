import { createFileRoute } from "@tanstack/react-router";
import { RemoteSessionView } from "../components/RemoteSessionView";

export const Route = createFileRoute("/_shell/host/$hostId/session/$sessionId")({
	component: HostSessionRoute,
});

function HostSessionRoute() {
	const { hostId, sessionId } = Route.useParams();
	return <RemoteSessionView key={`${hostId}:${sessionId}`} hostId={hostId} sessionId={sessionId} />;
}
