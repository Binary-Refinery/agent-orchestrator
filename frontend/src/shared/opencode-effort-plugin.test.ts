// @vitest-environment node
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import ts from "typescript";

// Exercise the actual managed Bun plugin. TypeScript erases the native Plugin
// type import; evaluating it with an isolated launch environment requires no
// OpenCode installation, provider network access, or user's runtime data.
const source = readFileSync(new URL("../../../backend/internal/adapters/agent/opencode/assets/ao-activity.ts", import.meta.url), "utf8");
const javascript = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
type NativeModel = { providerID: string; id: string; variants?: Record<string, Record<string, unknown>> };
type Hooks = {
	"chat.params": (input: { agent: string; model: NativeModel }, output: { options: Record<string, unknown> }) => Promise<void>;
	"chat.message": (input: { agent: string }, output: { message: Record<string, unknown> }) => Promise<void>;
};

async function plugin(env: Record<string, string>, agents = [
	{ name: "build", mode: "primary", hidden: false },
	{ name: "plan", mode: "primary", hidden: false },
	{ name: "custom-primary", mode: "primary", hidden: false },
	{ name: "custom-all", mode: "all", hidden: false },
	{ name: "explore", mode: "subagent", hidden: false },
	{ name: "title", mode: "primary", hidden: true },
	{ name: "compaction", mode: "primary", hidden: true },
	{ name: "summary", mode: "primary", hidden: true },
]): Promise<Hooks> {
	const exports: { aoActivity?: (context: unknown) => Promise<Hooks> } = {};
	new Function("exports", "process", "Bun", javascript)(exports, { env }, { which: () => null });
	return exports.aoActivity!({ directory: "/scratch/work", client: { app: { agents: async () => ({ data: agents }) } } });
}
const configured = { AO_OPENCODE_MODEL: "custom/reasoner", AO_OPENCODE_EFFORT: "contributor", AO_OPENCODE_AGENT: "ao-work-1" };
const model: NativeModel = { providerID: "custom", id: "reasoner", variants: { minimal: { reasoningEffort: "low" }, contributor: { reasoningEffort: "high", thinkingBudget: 9000 } } };

describe("OpenCode native effort enforcement", () => {
	it("overrides persisted minimal variant options before inference and records the native choice", async () => {
		const hooks = await plugin(configured);
		const output = { options: { reasoningEffort: "low", unrelated: true } };
		await hooks["chat.params"]({ agent: "ao-work-1", model }, output);
		expect(output.options).toEqual({ reasoningEffort: "high", thinkingBudget: 9000, unrelated: true });
		const message = { message: {} };
		await hooks["chat.message"]({ agent: "ao-work-1" }, message);
		expect(message.message).toEqual({ variant: "contributor" });
	});
	it("refuses unavailable, disabled, and drifted model choices", async () => {
		const hooks = await plugin(configured);
		for (const invalid of [
			{ ...model, variants: undefined },
			{ ...model, variants: { contributor: { disabled: true } } },
			{ ...model, id: "different" },
		]) {
			await expect(hooks["chat.params"]({ agent: "ao-work-1", model: invalid }, { options: {} })).rejects.toThrow(/AO configured/);
		}
	});
	it("refuses native build/plan switching that bypasses the configured AO role", async () => {
		const hooks = await plugin(configured);
		for (const agent of ["build", "plan", "custom-primary", "custom-all", "unknown"]) {
			await expect(hooks["chat.params"]({ agent, model }, { options: {} })).rejects.toThrow(/would bypass/);
			await expect(hooks["chat.message"]({ agent }, { message: {} })).rejects.toThrow(/would bypass/);
		}
	});

	it("keeps other model-specific title and compaction agents independent", async () => {
		const hooks = await plugin(configured);
		for (const agent of ["title", "compaction", "summary", "explore"]) {
			const output = { options: { temperature: 0.2 } };
			await hooks["chat.params"]({ agent, model: { providerID: "other", id: "plain" } }, output);
			expect(output.options).toEqual({ temperature: 0.2 });
		}
	});
	it("does not exempt a user-selectable agent just because it is named title", async () => {
		const hooks = await plugin(configured, [{ name: "title", mode: "primary", hidden: false }]);
		await expect(hooks["chat.params"]({ agent: "title", model }, { options: {} })).rejects.toThrow(/would bypass/);
	});
	it("preserves unmanaged OpenCode defaults", async () => {
		const hooks = await plugin({});
		const output = { options: { reasoningEffort: "minimal" } };
		await hooks["chat.params"]({ agent: "build", model }, output);
		expect(output.options.reasoningEffort).toBe("minimal");
	});
});
