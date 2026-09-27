import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { prepareDesktopReleaseNotes, PRODUCT_CHANGELOG_URL } from "../lib/desktop-release-notes";
import { ProductExternalLink } from "./ProductExternalLink";

export function DesktopReleaseNotes({ notes, textClassName }: {
	notes: string;
	textClassName: string;
}) {
	const { t } = useTranslation();
	const prepared = useMemo(() => prepareDesktopReleaseNotes(notes), [notes]);

	return (
		<>
			{prepared.text ? <p className={textClassName}>{prepared.text}</p> : null}
			{prepared.showChangelogLink ? (
				<ProductExternalLink
					href={PRODUCT_CHANGELOG_URL}
					className="mt-2 inline-flex text-sm text-settings-label underline decoration-settings-muted underline-offset-4 transition-colors hover:text-foreground"
				>
					{t("update.restart.viewChangelog")}
				</ProductExternalLink>
			) : null}
		</>
	);
}
