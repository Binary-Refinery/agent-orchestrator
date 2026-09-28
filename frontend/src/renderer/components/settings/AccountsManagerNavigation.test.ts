import { describe, expect, it } from "vitest";
import { createAppI18n } from "../../i18n/instance";
import { globalSettingsItem } from "./settingsCatalog";

describe("Accounts Manager navigation", () => {
	it.each([
		["en", "Accounts"],
		["es", "Cuentas"],
	] as const)("localizes the Accounts navigation label in %s", (locale, label) => {
		const item = globalSettingsItem("accounts", { cloudEnabled: false });
		expect(item.label(createAppI18n(locale).t)).toBe(label);
	});
});
