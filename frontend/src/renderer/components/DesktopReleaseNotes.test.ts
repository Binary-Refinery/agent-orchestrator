import { expect, it } from "vitest";
import { prepareDesktopReleaseNotes, splitDesktopReleaseNoteLinks } from "../lib/desktop-release-notes";

it("removes contributor handles and the comparison footer while retaining linked PR numbers", () => {
	const notes = [
		"### Added",
		"",
		"- Add useful workflows by @person in [#123](https://github.com/Untrivial-ai/agent-orchestrator/pull/123)",
		"- Update dependencies by @dependabot[bot] in [#124](https://github.com/Untrivial-ai/agent-orchestrator/pull/124)",
		"",
		"**Full Changelog**: https://github.com/Untrivial-ai/agent-orchestrator/compare/v0.13.0...v0.13.1",
	].join("\n");

	expect(prepareDesktopReleaseNotes(notes)).toBe([
		"### Added",
		"",
		"- Add useful workflows [#123](https://github.com/Untrivial-ai/agent-orchestrator/pull/123)",
		"- Update dependencies [#124](https://github.com/Untrivial-ai/agent-orchestrator/pull/124)",
	].join("\n"));
});

it("leaves nightly notes and unrelated user text unchanged", () => {
	const notes = "This automated build tracks main.\n\n- Source: @team in #general";
	expect(prepareDesktopReleaseNotes(notes)).toBe(notes);
});

it("turns only canonical AO pull request markdown into link parts", () => {
	const url = "https://github.com/Untrivial-ai/agent-orchestrator/pull/123";
	expect(splitDesktopReleaseNoteLinks(`Fixed [#123](${url}) and [docs](https://example.com)`)).toEqual([
		{ text: "Fixed " },
		{ text: "#123", href: url },
		{ text: " and [docs](https://example.com)" },
	]);
});
