import { expect, it } from "vitest";
import { prepareDesktopReleaseNotes } from "../lib/desktop-release-notes";

it("removes repository attribution from stable notes and replaces the comparison footer", () => {
	const notes = [
		"### Added",
		"",
		"- Add useful workflows by @person in [#123](https://github.com/Untrivial-ai/agent-orchestrator/pull/123)",
		"- Update dependencies by @dependabot[bot] in [#124](https://github.com/Untrivial-ai/agent-orchestrator/pull/124)",
		"",
		"**Full Changelog**: https://github.com/Untrivial-ai/agent-orchestrator/compare/v0.13.0...v0.13.1",
	].join("\n");

	expect(prepareDesktopReleaseNotes(notes)).toEqual({
		text: "### Added\n\n- Add useful workflows\n- Update dependencies",
		showChangelogLink: true,
	});
});

it("leaves nightly notes and unrelated user text unchanged", () => {
	const notes = "This automated build tracks main.\n\n- Source: @team in #general";
	expect(prepareDesktopReleaseNotes(notes)).toEqual({
		text: notes,
		showChangelogLink: false,
	});
});
