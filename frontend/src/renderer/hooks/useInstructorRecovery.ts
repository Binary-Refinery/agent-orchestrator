import { useMutation, useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient, apiErrorCode, apiErrorDetails, apiErrorMessage, apiErrorRequestId } from "../lib/api-client";
import { instructorQueryKey } from "./useInstructor";

// Keep correlation and daemon classification visible for actionable recovery.
export class InstructorRecoveryError extends Error {
	readonly code: string | undefined;
	readonly requestId: string | undefined;
	readonly details: Record<string, unknown> | undefined;
	constructor(error: unknown) {
		const code = apiErrorCode(error), requestId = apiErrorRequestId(error);
		super([apiErrorMessage(error), code, requestId].filter(Boolean).join(" · "));
		this.code = code; this.requestId = requestId; this.details = apiErrorDetails(error);
	}
}

export type InstructorPolicyRebindInput = components["schemas"]["InstructorPolicyRebindRequest"];
export type InstructorPolicyRebindPreview = components["schemas"]["InstructorPolicyRebindPreview"];

export function useInstructorEffectivePolicy(projectId: string | null) {
	return useQuery({
		queryKey: [...instructorQueryKey, "effective-policy", projectId],
		queryFn: async ({ signal }) => {
			if (!projectId) return null;
			const { data, error } = await apiClient.GET("/api/v1/instructor/projects/{projectId}/policy", { params: { path: { projectId } }, signal });
			if (error) throw new InstructorRecoveryError(error);
			if (!data?.policy) throw new Error("Effective project policy response was empty");
			return data;
		},
		enabled: Boolean(projectId), retry: false, refetchInterval: 15_000,
	});
}

export function useInstructorPolicyRebindPreview() {
	return useMutation({ mutationFn: async (body: InstructorPolicyRebindInput) => {
		const { data, error } = await apiClient.POST("/api/v1/instructor/policy-rebind/preview", { body });
		if (error) throw new InstructorRecoveryError(error);
		if (!data) throw new Error("Policy rebind preview was empty");
		return data;
	}, retry: false });
}

export function useApplyInstructorPolicyRebind() {
	return useMutation({ mutationFn: async (body: InstructorPolicyRebindInput) => {
		const { data, error } = await apiClient.POST("/api/v1/instructor/policy-rebind", { body });
		if (error) throw new InstructorRecoveryError(error);
		if (!data) throw new Error("Policy rebind response was empty");
		return data;
	}, retry: false });
}

export type InstructorRecoveryStatus = components["schemas"]["InstructorRecoveryStatus"];
export type InstructorRecoveryInput = components["schemas"]["InstructorRecoveryRequest"];

export function useInstructorRecoveryPreview() {
	return useMutation({ mutationFn: async (workId: string) => {
		const { data, error } = await apiClient.GET("/api/v1/instructor/works/{workId}/recovery", { params: { path: { workId } } });
		if (error) throw new InstructorRecoveryError(error);
		if (!data) throw new Error("Session recovery response was empty");
		return data;
	}, retry: false });
}

export function useRecoverInstructorSession() {
	return useMutation({ mutationFn: async (body: InstructorRecoveryInput) => {
		const { data, error } = await apiClient.POST("/api/v1/instructor/works/{workId}/recovery", { params: { path: { workId: body.workId } }, body });
		if (error) throw new InstructorRecoveryError(error);
		if (!data) throw new Error("Session recovery response was empty");
		return data;
	}, retry: false });
}
