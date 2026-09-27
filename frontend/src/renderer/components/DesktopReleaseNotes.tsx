import { useMemo } from "react";
import { prepareDesktopReleaseNotes, splitDesktopReleaseNoteLinks } from "../lib/desktop-release-notes";
import { ProductExternalLink } from "./ProductExternalLink";

export function DesktopReleaseNotes({ notes, textClassName }: {
	notes: string;
	textClassName: string;
}) {
	const prepared = useMemo(() => prepareDesktopReleaseNotes(notes), [notes]);
	const parts = useMemo(() => splitDesktopReleaseNoteLinks(prepared), [prepared]);

	if (!prepared) return null;

	return (
		<p className={textClassName}>
			{parts.map((part, index) => part.href ? (
				<ProductExternalLink
					key={`${part.href}-${index}`}
					href={part.href}
					className="underline decoration-settings-muted underline-offset-2 transition-colors hover:text-foreground"
				>
					{part.text}
				</ProductExternalLink>
			) : part.text)}
		</p>
	);
}
