export const PRODUCT_CHANGELOG_URL = "https://orchestrator.inc/changelog";

const RELEASE_ATTRIBUTION_PATTERN =
	/\s+by\s+@[A-Za-z0-9-]+(?:\[bot\])?\s+in\s+\[#\d+\]\(https:\/\/github\.com\/Untrivial-ai\/agent-orchestrator\/pull\/\d+\)\s*$/;
const FULL_CHANGELOG_PATTERN =
	/^\*\*Full Changelog\*\*:\s+https:\/\/github\.com\/Untrivial-ai\/agent-orchestrator\/(?:compare|commits)\/\S+\s*$/i;

export function prepareDesktopReleaseNotes(notes: string): {
	text: string;
	showChangelogLink: boolean;
} {
	let showChangelogLink = false;
	const lines = notes.split(/\r?\n/).flatMap((line) => {
		if (FULL_CHANGELOG_PATTERN.test(line.trim())) {
			showChangelogLink = true;
			return [];
		}
		return line.replace(RELEASE_ATTRIBUTION_PATTERN, "").trimEnd();
	});

	return {
		text: lines.join("\n").replace(/\n{3,}/g, "\n\n").trim(),
		showChangelogLink,
	};
}
