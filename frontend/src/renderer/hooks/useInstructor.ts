import { useMutation, useQuery } from "@tanstack/react-query";
import type { components, paths } from "../../api/schema";
import { apiClient, apiErrorMessage } from "../lib/api-client";

export type InstructorWork = components["schemas"]["InstructorWork"];
export type InstructorSnapshot = components["schemas"]["InstructorSnapshot"];
export type InstructorCommandSpec = components["schemas"]["InstructorCommandSpec"];
export type InstructorPolicy = components["schemas"]["InstructorPolicy"];
export type InstructorCommandInput = NonNullable<paths["/api/v1/instructor/commands"]["post"]["requestBody"]>["content"]["application/json"];
export type InstructorReconcileInput = components["schemas"]["InstructorReconcileRequest"];
export type InstructorExceptionInput = NonNullable<paths["/api/v1/instructor/exceptions/preview"]["post"]["requestBody"]>["content"]["application/json"];
export type InstructorExceptionPreview = paths["/api/v1/instructor/exceptions/preview"]["post"]["responses"][200]["content"]["application/json"];

export const instructorQueryKey = ["instructor"] as const;

export function useInstructorCapabilities() {
	return useQuery({
		queryKey: [...instructorQueryKey, "capabilities"],
		queryFn: async () => {
			const { data, error } = await apiClient.GET("/api/v1/instructor/capabilities");
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Instructor capability response was empty");
			return data;
		},
		retry: false,
	});
}

// Operator commands refer to this exact snapshot. Background refetches must not
// silently substitute a different cohort while the operator is deciding.
export function useInstructorSnapshot(enabled: boolean) {
	return useQuery({
		queryKey: [...instructorQueryKey, "snapshot"],
		queryFn: async () => {
			const { data, error } = await apiClient.GET("/api/v1/instructor/status");
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Instructor status response was empty");
			return data;
		},
		enabled,
		retry: false,
		refetchOnWindowFocus: false,
		refetchOnReconnect: false,
		staleTime: Infinity,
	});
}

export function useInstructorInventory(enabled: boolean) {
	return useQuery({
		queryKey: [...instructorQueryKey, "inventory"],
		queryFn: async () => {
			const { data, error } = await apiClient.GET("/api/v1/instructor/inventory");
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Instructor inventory response was empty");
			return data;
		},
		enabled,
		retry: false,
		refetchInterval: 15_000,
	});
}

export function useInstructorDetails(workId: string | null) {
	return useQuery({
		queryKey: [...instructorQueryKey, "details", workId],
		queryFn: async () => {
			if (!workId) return null;
			const { data, error } = await apiClient.GET("/api/v1/instructor/works/{workId}", { params: { path: { workId } } });
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Instructor details response was empty");
			return data;
		},
		enabled: Boolean(workId),
		retry: false,
		refetchInterval: 15_000,
	});
}

export function useInstructorCommand() {
	return useMutation({
		mutationFn: async (body: InstructorCommandInput) => {
			const { data, error } = await apiClient.POST("/api/v1/instructor/commands", { body });
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Instructor command response was empty");
			return data;
		},
		retry: false,
	});
}

export function useInstructorPolicy(enabled: boolean) {
	return useQuery({
		queryKey: [...instructorQueryKey, "policy"],
		queryFn: async () => {
			const { data, error } = await apiClient.GET("/api/v1/instructor/policy");
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Instructor policy response was empty");
			return data;
		},
		enabled,
		retry: false,
		refetchOnWindowFocus: false,
	});
}

export function useSaveInstructorPolicy() {
	return useMutation({
		mutationFn: async (body: InstructorPolicy) => {
			const { data, error } = await apiClient.PUT("/api/v1/instructor/policy", { body });
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Instructor policy response was empty");
			return data;
		},
		retry: false,
	});
}

export function useInstructorReconcile() {
	return useMutation({
		mutationFn: async (body: InstructorReconcileInput) => {
			const { data, error } = await apiClient.POST("/api/v1/instructor/reconcile", { body });
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Instructor reconciliation response was empty");
			return data;
		},
		retry: false,
	});
}

export function useInstructorExceptionPreview() {
	return useMutation({
		mutationFn: async (body: InstructorExceptionInput) => {
			const { data, error } = await apiClient.POST("/api/v1/instructor/exceptions/preview", { body });
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Instructor exception preview was empty");
			return data;
		},
		retry: false,
	});
}

export function useApplyInstructorException() {
	return useMutation({
		mutationFn: async (body: InstructorExceptionInput) => {
			const { data, error } = await apiClient.POST("/api/v1/instructor/exceptions", { body });
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Instructor exception response was empty");
			return data;
		},
		retry: false,
	});
}

export type InstructorPlanJob = components["schemas"]["InstructorPlanJob"];
export type InstructorProposal = components["schemas"]["InstructorProposal"];
export type InstructorPlanIntake = components["schemas"]["InstructorPlanIntake"];
export type InstructorPlanInput = NonNullable<paths["/api/v1/instructor/plans"]["post"]["requestBody"]>["content"]["application/json"];
export type InstructorInventoryReconcileInput = NonNullable<paths["/api/v1/instructor/inventory/reconcile"]["post"]["requestBody"]>["content"]["application/json"];

export function useInstructorInventoryReconcile() {
	return useMutation({
		mutationFn: async (body: InstructorInventoryReconcileInput) => {
			const { data, error } = await apiClient.POST("/api/v1/instructor/inventory/reconcile", { body });
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Instructor inventory reconciliation response was empty");
			return data;
		},
		retry: false,
	});
}

export function useInstructorPlans() {
	return useQuery({
		queryKey: [...instructorQueryKey, "plans"],
		queryFn: async () => {
			const { data, error } = await apiClient.GET("/api/v1/instructor/plans");
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Instructor planning history response was empty");
			return data;
		},
		retry: false,
		refetchInterval: (query) => query.state.data?.jobs?.some((job) => job.state === "queued" || job.state === "running") ? 3_000 : 15_000,
	});
}

export function useCreateInstructorPlan() {
	return useMutation({
		mutationFn: async (body: InstructorPlanInput) => {
			const { data, error } = await apiClient.POST("/api/v1/instructor/plans", { body });
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Instructor planning response was empty");
			return data;
		},
		retry: false,
	});
}
