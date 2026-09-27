const RELEASE_ATTRIBUTION_PATTERN =
	/\s+by\s+@[A-Za-z0-9-]+(?:\[bot\])?\s+in\s+(\[#\d+\]\(https:\/\/github\.com\/Untrivial-ai\/agent-orchestrator\/pull\/\d+\))\s*$/;
const FULL_CHANGELOG_PATTERN =
	/^\*\*Full Changelog\*\*:\s+https:\/\/github\.com\/Untrivial-ai\/agent-orchestrator\/(?:compare|commits)\/\S+\s*$/i;
const PULL_REQUEST_LINK_PATTERN =
	/\[#(\d+)\]\((https:\/\/github\.com\/Untrivial-ai\/agent-orchestrator\/pull\/\d+)\)/g;

export function prepareDesktopReleaseNotes(notes: string): string {
	const lines = notes.split(/\r?\n/).flatMap((line) => {
		if (FULL_CHANGELOG_PATTERN.test(line.trim())) {
			return [];
		}
		return line.replace(RELEASE_ATTRIBUTION_PATTERN, " $1").trimEnd();
	});

	return lines.join("\n").replace(/\n{3,}/g, "\n\n").trim();
}

export function splitDesktopReleaseNoteLinks(text: string): Array<{
	text: string;
	href?: string;
}> {
	const parts: Array<{ text: string; href?: string }> = [];
	let cursor = 0;

	for (const match of text.matchAll(PULL_REQUEST_LINK_PATTERN)) {
		const index = match.index ?? 0;
		if (index > cursor) parts.push({ text: text.slice(cursor, index) });
		parts.push({ text: `#${match[1]}`, href: match[2] });
		cursor = index + match[0].length;
	}

	if (cursor < text.length) parts.push({ text: text.slice(cursor) });
	return parts;
}
