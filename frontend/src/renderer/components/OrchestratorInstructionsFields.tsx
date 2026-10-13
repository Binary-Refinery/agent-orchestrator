import { useTranslation } from "react-i18next";
import { Input } from "./ui/input";
import { Label } from "./ui/label";

type InstructionsPatch = Partial<{
	orchestratorRules: string;
	orchestratorRulesFile: string;
	orchestratorPrompt: string;
}>;

export function OrchestratorInstructionsFields({
	idPrefix,
	rules,
	rulesFile,
	prompt,
	disabled = false,
	onChange,
}: {
	idPrefix: string;
	rules: string;
	rulesFile: string;
	prompt: string;
	disabled?: boolean;
	onChange: (patch: InstructionsPatch) => void;
}) {
	const { t } = useTranslation();
	const textareaClass = "min-h-20 w-full resize-y rounded-md border border-input bg-transparent px-3 py-2 text-sm leading-5 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50";
	return (
		<details className="min-w-0 rounded-lg border border-border p-3">
			<summary className="cursor-pointer text-sm font-medium text-settings-label">
				{t("settings.project.orchestratorInstructions")}
			</summary>
			<div className="mt-3 space-y-4">
				<div className="space-y-1.5">
					<Label htmlFor={`${idPrefix}-rules`}>{t("settings.project.orchestratorRules")}</Label>
					<textarea id={`${idPrefix}-rules`} className={textareaClass} disabled={disabled} value={rules}
						aria-describedby={`${idPrefix}-rules-help`} onChange={(event) => onChange({ orchestratorRules: event.target.value })} />
					<p id={`${idPrefix}-rules-help`} className="text-xs leading-5 text-settings-muted">{t("settings.project.orchestratorRulesDescription")}</p>
				</div>
				<div className="space-y-1.5">
					<Label htmlFor={`${idPrefix}-rules-file`}>{t("settings.project.orchestratorRulesFile")}</Label>
					<Input id={`${idPrefix}-rules-file`} disabled={disabled} value={rulesFile} placeholder={t("settings.project.orchestratorRulesFilePlaceholder")}
						aria-describedby={`${idPrefix}-rules-file-help`} onChange={(event) => onChange({ orchestratorRulesFile: event.target.value })} />
					<p id={`${idPrefix}-rules-file-help`} className="text-xs leading-5 text-settings-muted">{t("settings.project.orchestratorRulesFileDescription")}</p>
				</div>
				<div className="space-y-1.5">
					<Label htmlFor={`${idPrefix}-prompt`}>{t("settings.project.orchestratorPrompt")}</Label>
					<textarea id={`${idPrefix}-prompt`} className={textareaClass} disabled={disabled} value={prompt}
						aria-describedby={`${idPrefix}-prompt-help`} onChange={(event) => onChange({ orchestratorPrompt: event.target.value })} />
					<p id={`${idPrefix}-prompt-help`} className="text-xs leading-5 text-settings-muted">{t("settings.project.orchestratorPromptDescription")}</p>
				</div>
			</div>
		</details>
	);
}
