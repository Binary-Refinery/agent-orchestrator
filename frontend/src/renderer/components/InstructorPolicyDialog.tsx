import { useCallback, useEffect, useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useAgentReadinessQuery } from "../hooks/useAgentReadinessQuery";
import { agentModelsQueryOptions, refreshAgentModels } from "../hooks/useAgentModelsQuery";
import { instructorQueryKey, useInstructorPolicy, useSaveInstructorPolicy, type InstructorPolicy } from "../hooks/useInstructor";
import { AgentModelCombobox } from "./settings/AgentModelCombobox";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "./ui/dialog";

type RoleSettings = NonNullable<InstructorPolicy["roles"]>[string];

// The daemon supplies this policy, validates saves and pins it to new work.
// This editor only changes explicit role selections and executable profiles.
export function InstructorPolicyDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
	const { t } = useTranslation();
	const client = useQueryClient();
	const query = useInstructorPolicy(open);
	const save = useSaveInstructorPolicy();
	const [draft, setDraft] = useState<InstructorPolicy | null>(null);
	const [invalidRoles, setInvalidRoles] = useState<Record<string, boolean>>({});
	const [roleName, setRoleName] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [saved, setSaved] = useState(false);

	useEffect(() => {
		if (open && query.data) {
			setDraft(structuredClone(query.data));
			setInvalidRoles({});
			setError(null);
		}
	}, [open, query.data]);

	function patchRole(name: string, role: RoleSettings) {
		setSaved(false);
		setDraft((current) => current ? { ...current, roles: { ...current.roles, [name]: role } } : current);
	}

	async function submit(event: FormEvent) {
		event.preventDefault();
		if (!draft) return;
		setError(null);
		try {
			const result = await save.mutateAsync(draft);
			client.setQueryData([...instructorQueryKey, "policy"], result);
			setDraft(result);
			setSaved(true);
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : t("instructor.saveFailed"));
		}
	}

	return <Dialog open={open} onOpenChange={(next) => { if (!next && !save.isPending) onClose(); }}><DialogContent className="max-h-[85vh] max-w-3xl overflow-y-auto"><DialogTitle>{t("instructor.policySettings")}</DialogTitle><DialogDescription>{t("instructor.policyHint")}</DialogDescription>{query.isLoading ? <p className="text-sm text-muted-foreground">{t("instructor.loading")}</p> : null}{query.error ? <p role="alert" className="text-sm text-destructive">{query.error.message}</p> : null}{draft ? <form className="space-y-5" onSubmit={(event) => void submit(event)}><label className="block text-sm" htmlFor="instructor-policy-version">{t("instructor.version")}</label><Input id="instructor-policy-version" readOnly value={draft.version} /><p className="text-xs text-muted-foreground">{t("instructor.maximumIntent")}</p><section className="space-y-3">{Object.entries(draft.roles ?? {}).map(([role, settings]) => <InstructorRoleEditor key={role} role={role} settings={settings} disabled={save.isPending} onChange={(next) => patchRole(role, next)} onValidityChange={(valid) => setInvalidRoles((current) => current[role] === !valid ? current : { ...current, [role]: !valid })} />)}<div className="flex flex-wrap gap-2"><Input aria-label={t("instructor.roleName")} value={roleName} onChange={(event) => setRoleName(event.target.value)} className="w-48" /><Button type="button" variant="outline" disabled={!roleName.trim() || Object.hasOwn(draft.roles ?? {}, roleName.trim())} onClick={() => { patchRole(roleName.trim(), { harness: "", model: "", effort: "maximum" }); setRoleName(""); }}>{t("instructor.addRole")}</Button></div></section><section className="space-y-3"><h3 className="font-medium">{t("instructor.profiles")}</h3><p className="text-xs text-muted-foreground">{t("instructor.profileHint")}</p>{Object.entries(draft.profiles ?? {}).map(([name, profile]) => <div key={name} className="rounded-md border border-border p-3"><label className="mb-2 block text-sm" htmlFor={`instructor-profile-${name}`}>{name}</label><textarea id={`instructor-profile-${name}`} className="min-h-20 w-full rounded-md border border-input bg-background p-2 font-mono text-xs" value={(profile.command ?? []).join("\n")} onChange={(event) => { setSaved(false); setDraft({ ...draft, profiles: { ...draft.profiles, [name]: { ...profile, command: event.target.value === "" ? [] : event.target.value.split("\n") } } }); }} /><label className="mt-2 block text-xs" htmlFor={`instructor-timeout-${name}`}>{t("instructor.timeout")}</label><Input id={`instructor-timeout-${name}`} type="number" min={1} required value={profile.timeoutSeconds} onChange={(event) => { setSaved(false); setDraft({ ...draft, profiles: { ...draft.profiles, [name]: { ...profile, timeoutSeconds: Number(event.target.value) } } }); }} className="mt-1 w-32" /></div>)}</section>{Object.values(invalidRoles).some(Boolean) ? <p role="alert" className="text-sm text-warning">{t("instructor.missingMapping")}</p> : null}{error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}{saved ? <p role="status" className="text-sm">{t("instructor.policySaved")}</p> : null}<Button type="submit" disabled={save.isPending || Object.values(invalidRoles).some(Boolean)}>{t("instructor.savePolicy")}</Button></form> : null}</DialogContent></Dialog>;
}

export function InstructorRoleEditor({ role, settings, disabled, onChange, onValidityChange }: { role: string; settings: RoleSettings; disabled: boolean; onChange: (next: RoleSettings) => void; onValidityChange: (valid: boolean) => void }) {
	const { t } = useTranslation();
	const client = useQueryClient();
	const agents = useAgentReadinessQuery().data?.agents ?? [];
	const query = useQuery(agentModelsQueryOptions(settings.harness, ""));
	const nativeEffort = settings.effortByModel?.[settings.model] ?? (settings.effort !== "maximum" && settings.effort !== "maximum-native" ? settings.effort : "");
	const [tuningValid, setTuningValid] = useState(true);
	const [refreshError, setRefreshError] = useState<string | undefined>();
	const [refreshing, setRefreshing] = useState(false);
	const onTuningValidity = useCallback((valid: boolean) => setTuningValid(valid), []);
	const validity = Boolean(settings.harness && settings.model && nativeEffort && tuningValid && query.data?.models?.some((model) => model.id === settings.model && model.efforts?.includes(nativeEffort)));

	useEffect(() => onValidityChange(validity), [onValidityChange, validity]);

	function selectModel(value: string) {
		// The shared project picker represents catalog defaults with an empty
		// string. Policy mappings need the concrete native model identifier.
		const model = value || query.data?.models?.find((item) => item.isDefault)?.id || "";
		onChange({ ...settings, model });
	}

	function selectEffort(effort: string) {
		if (!settings.model) return;
		onChange({ ...settings, effort: "maximum", effortByModel: { ...settings.effortByModel, [settings.model]: effort } });
	}

	async function refresh() {
		setRefreshing(true);
		setRefreshError(undefined);
		try {
			const catalog = await refreshAgentModels(settings.harness, "");
			client.setQueryData(agentModelsQueryOptions(settings.harness, "").queryKey, catalog);
		} catch (cause) { setRefreshError(cause instanceof Error ? cause.message : t("instructor.policyNotLoaded")); }
		finally { setRefreshing(false); }
	}

	return <div className="rounded-md border border-border p-3"><h3 className="mb-3 font-medium">{role}</h3><div className="flex flex-wrap items-center gap-3"><label className="flex items-center gap-2 text-xs text-muted-foreground">{t("instructor.harness")}<select aria-label={`${role} ${t("instructor.harness")}`} value={settings.harness} disabled={disabled} onChange={(event) => onChange({ ...settings, harness: event.target.value, model: "", effortByModel: settings.effortByModel })} className="rounded-md border border-input bg-background px-2 py-1.5 text-sm text-foreground"><option value="">{t("instructor.unknown")}</option>{settings.harness && !agents.some((agent) => agent.id === settings.harness) ? <option value={settings.harness}>{settings.harness}</option> : null}{agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.label}</option>)}</select></label><AgentModelCombobox aria-label={`${role} ${t("instructor.requested")}`} value={settings.model} models={query.data?.models ?? []} allowCustom={query.data?.allowCustom} customModelEntry={query.data?.customModelEntry} onChange={selectModel} onCustom={selectModel} onRefresh={() => void refresh()} refreshing={refreshing} refreshError={refreshError} disabled={disabled || !settings.harness} showFollowAgentAction={false} pinSelection tuning={{ effort: nativeEffort, requireExplicitEffort: true, onEffortChange: selectEffort, onValidityChange: onTuningValidity, roleLabel: role }} /></div><p className="mt-2 text-xs text-muted-foreground">{t("instructor.nativeEffort")} · {nativeEffort || t("instructor.unknown")}</p>{query.isLoading ? <p className="mt-2 text-xs text-muted-foreground">{t("instructor.catalogLoading")}</p> : null}{query.error ? <p role="alert" className="mt-2 text-xs text-destructive">{query.error.message}</p> : null}</div>;
}
