import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useEffect, useRef, useState } from "react";
import { agentModelsQueryKey, agentModelsQueryOptions, refreshAgentModels, revalidateAgentModels, type AgentModelCatalog } from "../../hooks/useAgentModelsQuery";
import { isConcreteModelID, modelChoiceLabel } from "../../lib/agent-model-choices";
import { AgentModelCombobox } from "./AgentModelCombobox";
import { SettingsOptionMenu } from "./SettingsOptionMenu";

export function ProjectAgentModelField({
	role,
	disabled = false,
	agentId,
	projectId,
	model,
	mode,
	effort,
	onModelChange,
	onModeChange,
	onEffortChange,
	onValidityChange,
}: {
	role: "worker" | "orchestrator" | "reviewer";
	disabled?: boolean;
	agentId: string;
	projectId: string;
	model: string;
	mode: string;
	effort: string;
	onModelChange: (value: string) => void;
	onModeChange: (value: string) => void;
	onEffortChange: (value: string) => void;
	onValidityChange: (valid: boolean) => void;
}) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const query = useQuery(agentModelsQueryOptions(agentId, projectId));
	const catalog: AgentModelCatalog | undefined = query.data;
	const [tuningValid, setTuningValid] = useState(true);
	const validityCallback = useRef(onValidityChange);
	validityCallback.current = onValidityChange;
	const nativeModel = (catalog?.models ?? []).find((item) => item.id === model) ?? (!model ? catalog?.models?.find((item) => item.isDefault && isConcreteModelID(item.id)) : undefined);
	const needsExplicitEffort = Boolean(nativeModel?.efforts?.some((value) => value && (nativeModel.effortSupport === "supported" || value.toLowerCase() !== "default")));
	const nativeValid = tuningValid && !(needsExplicitEffort && Boolean(model) && !effort) && !(nativeModel?.effortSupport === "unknown" && Boolean(model || effort)) && !(catalog?.stale && Boolean(effort));
	useEffect(() => { validityCallback.current(nativeValid); }, [nativeValid]);
	const revalidationQuery = useQuery({
		queryKey: ["agent-model-revalidation", agentId, projectId, catalog?.validatedAt ?? ""],
		queryFn: () => revalidateAgentModels(agentId, projectId),
		enabled: agentId !== "" && catalog?.refreshRecommended === true,
		staleTime: Number.POSITIVE_INFINITY,
		retry: false,
	});
	useEffect(() => {
		if (revalidationQuery.data) {
			queryClient.setQueryData(agentModelsQueryKey(agentId, projectId), revalidationQuery.data);
		}
	}, [agentId, projectId, queryClient, revalidationQuery.data]);
	const isMode = catalog?.selectionMode === "mode";
	const label = t(`settings.models.${role}${isMode ? "Mode" : "Model"}`);
	const warning =
		(revalidationQuery.isError ? (revalidationQuery.error instanceof Error ? revalidationQuery.error.message : t("settings.models.validateFailed")) : undefined) ??
		catalog?.warning ??
		(query.isError ? (query.error instanceof Error ? query.error.message : t("settings.models.loadFailed")) : undefined);

	if (agentId !== "" && query.isFetching && catalog === undefined) {
		return (
			<div className="min-w-0">
				<span className="text-xs text-settings-muted" role="status" aria-label={t("settings.models.loading")}>
					{t("settings.models.loading")}
				</span>
			</div>
		);
	}

	if (isMode) {
		const defaultMode = catalog.models?.find((item) => item.isDefault && isConcreteModelID(item.id))?.id;
		const selectedMode = isConcreteModelID(mode) ? mode : "";
		const options = (catalog.models ?? []).filter((item) => isConcreteModelID(item.id)).map((item) => ({
			value: item.id,
			label: modelChoiceLabel(item),
		}));
		return (
			<>
				<div className="min-w-0">
					<div className="flex min-w-0 items-center gap-2">
						<SettingsOptionMenu
							aria-label={label}
							value={selectedMode || defaultMode || ""}
							options={options}
							placeholder={t("settings.models.modeNotReported")}
							action={selectedMode && !defaultMode ? { label: t("settings.models.useAgentMode"), onSelect: () => onModeChange("") } : undefined}
							triggerClassName="w-full justify-between"
							disabled={disabled || (options.length === 0 && !(selectedMode && !defaultMode))}
							onChange={(value) => {
								onModeChange(value === defaultMode ? "" : value);
								onModelChange("");
							}}
						/>
					</div>
				</div>
				{warning && <p className="px-1 text-xs leading-row text-warning">{warning}</p>}
			</>
		);
	}

	const customModelEntry = catalog?.customModelEntry ?? (catalog?.allowCustom ? "direct" : "none");
	const refreshCatalog = async () => {
		const refreshed = await refreshAgentModels(agentId, projectId);
		queryClient.setQueryData(agentModelsQueryKey(agentId, projectId), refreshed);
	};
	const selectCatalogModel = (value: string) => {
		onModelChange(value || catalog?.models?.find((item) => item.isDefault && isConcreteModelID(item.id))?.id || "");
		onModeChange("");
	};
	const selectCustomModel = (value: string) => {
		onModelChange(value);
		onModeChange("");
	};
	const chooseNativeEffort = (value: string) => {
		if (!model && nativeModel?.id && value) onModelChange(nativeModel.id);
		onEffortChange(value);
	};
	return (
		<>
			<div className="min-w-0">
				<div className="flex min-w-0 items-center gap-2">
					<AgentModelCombobox
						aria-label={label}
						value={model}
						pinSelection
						models={catalog?.models ?? []}
						allowCustom={catalog?.allowCustom}
						customModelEntry={customModelEntry}
						agentLabel={agentId}
						onRefresh={refreshCatalog}
						refreshing={catalog?.refreshState === "queued" || catalog?.refreshState === "refreshing"}
						refreshError={catalog?.refreshError}
						retryAt={catalog?.retryAt}
						disabled={disabled || (query.isFetching && !catalog) || agentId === ""}
						onChange={selectCatalogModel}
						onCustom={selectCustomModel}
						triggerClassName="w-full justify-between"
						compact={agentId === "codex"}
						tuning={{
							effort,
							requireExplicitEffort: true,
							onEffortChange: chooseNativeEffort,
							onValidityChange: setTuningValid,
							roleLabel: t(`settings.models.${role}Role`),
						}}
					/>
				</div>
			</div>
			{warning && <p className="px-1 text-xs leading-row text-warning">{warning}</p>}
			{needsExplicitEffort && Boolean(model) && !effort ? <p className="px-1 text-xs leading-row text-warning">{t("settings.models.nativeDefaultRequired")}</p> : null}
		</>
	);
}

