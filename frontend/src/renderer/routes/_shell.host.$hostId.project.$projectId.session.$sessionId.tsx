import { createFileRoute } from "@tanstack/react-router";
import { RemoteSessionRoute } from "../components/RemoteSessionView";
import { refKey } from "../lib/hosts";

export const Route = createFileRoute("/_shell/host/$hostId/project/$projectId/session/$sessionId")({
	component: HostProjectSessionRoute,
});

function HostProjectSessionRoute() {
	const { hostId, sessionId } = Route.useParams();
	return <RemoteSessionRoute key={refKey({ host: hostId, id: sessionId })} hostId={hostId} sessionId={sessionId} />;
}
