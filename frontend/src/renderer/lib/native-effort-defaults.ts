import type { components } from "../../api/schema";

export type NativeEffortDefaults = NonNullable<components["schemas"]["AgentConfig"]["nativeEffortDefaults"]>;

export function mergeNativeEffortDefaults(...sources: Array<NativeEffortDefaults | null | undefined>): NativeEffortDefaults {
	const result: NativeEffortDefaults = {};
	for (const source of sources) {
		for (const [harness, models] of Object.entries(source ?? {})) result[harness] = { ...result[harness], ...models };
	}
	return result;
}

export function rememberNativeEffort(defaults: NativeEffortDefaults, harness: string, model: string, effort: string): NativeEffortDefaults {
	if (!harness || !model || !effort) return defaults;
	return { ...defaults, [harness]: { ...defaults[harness], [model]: effort } };
}

export function savedNativeEffort(defaults: NativeEffortDefaults, harness: string, model: string): string {
	return defaults[harness]?.[model] ?? "";
}

// Mirror role resolution when a stored preference supplies the native value.
// A different profiled model cannot inherit another model's shared effort.
export function initialNativeRoleSelection(harness: string, shared: components["schemas"]["AgentConfig"] | undefined, role: components["schemas"]["AgentConfig"] | undefined) {
	const defaults = mergeNativeEffortDefaults(shared?.nativeEffortDefaults, role?.nativeEffortDefaults);
	const model = role?.model || shared?.model || "";
	const changedProfiledModel = Boolean(role?.model && role.model !== shared?.model && Object.keys(defaults).length);
	const effort = role?.effort || (!changedProfiledModel ? shared?.effort : "") || savedNativeEffort(defaults, harness, model);
	return { model, effort, defaults: rememberNativeEffort(defaults, harness, model, effort) };
}
