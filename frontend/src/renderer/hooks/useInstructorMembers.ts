import { useQuery } from "@tanstack/react-query";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { instructorQueryKey } from "./useInstructor";

// Membership is a daemon-owned incarnation-checked projection. Never join
// historical session IDs against today's inventory in the renderer.
export function useInstructorMembers(workId: string | null) {
	return useQuery({
		queryKey: [...instructorQueryKey, "members", workId],
		queryFn: async ({ signal }) => {
			if (!workId) return null;
			const { data, error } = await apiClient.GET("/api/v1/instructor/works/{workId}/members", {
				params: { path: { workId } }, signal,
			});
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Instructor membership response was empty");
			return data;
		},
		enabled: Boolean(workId),
		retry: false,
		refetchInterval: 15_000,
	});
}
