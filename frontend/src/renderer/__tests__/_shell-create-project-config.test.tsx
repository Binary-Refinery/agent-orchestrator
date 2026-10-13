import { describe, expect, it } from "vitest";
import { createProjectConfig } from "../routes/_shell";

describe("createProjectConfig", () => {
	it("persists role model settings and instructions in the same creation config before any launch", () => {
		expect(createProjectConfig({
			workerAgent: "codex", orchestratorAgent: "opencode", defaultBranch: "release",
			workerAgentConfig: { model: "worker/model", effort: "high" },
			orchestratorAgentConfig: { model: "provider/orchestrator-model", effort: "native-deep", nativeEffortDefaults: { opencode: { "provider/orchestrator-model": "native-deep" } } },
			orchestratorRules: "Keep human gates.",
			orchestratorRulesFile: " .ao/orchestrator.md ",
			orchestratorPrompt: "Review assigned work.",
		})).toEqual({
			defaultBranch: "release",
			worker: { agent: "codex", agentConfig: { model: "worker/model", effort: "high" } },
			orchestrator: { agent: "opencode", agentConfig: { model: "provider/orchestrator-model", effort: "native-deep", nativeEffortDefaults: { opencode: { "provider/orchestrator-model": "native-deep" } } } },
			orchestratorRules: "Keep human gates.",
			orchestratorRulesFile: ".ao/orchestrator.md",
			orchestratorPrompt: "Review assigned work.",
		});
	});

	it("keeps blank optional instructions out of the default config", () => {
		expect(createProjectConfig({
			workerAgent: "codex", orchestratorAgent: "opencode",
			orchestratorRules: " ", orchestratorRulesFile: " ", orchestratorPrompt: "\n",
		})).toEqual({ worker: { agent: "codex" }, orchestrator: { agent: "opencode" } });
	});

	it("persists selected worker and orchestrator agents without tracker intake by default", () => {
		expect(
			createProjectConfig({
				workerAgent: "codex",
				orchestratorAgent: "claude-code",
			}),
		).toEqual({
			worker: { agent: "codex" },
			orchestrator: { agent: "claude-code" },
		});
	});

	it("preserves tracker intake alongside selected agent defaults", () => {
		expect(
			createProjectConfig({
				workerAgent: "cursor",
				orchestratorAgent: "opencode",
				trackerIntake: { enabled: true, provider: "github", assignee: "octocat" },
			}),
		).toEqual({
			worker: { agent: "cursor" },
			orchestrator: { agent: "opencode" },
			trackerIntake: { enabled: true, provider: "github", assignee: "octocat" },
		});
	});
});
