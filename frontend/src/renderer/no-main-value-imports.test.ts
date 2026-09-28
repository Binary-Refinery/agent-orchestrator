import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

// The renderer runs in Chromium. Modules under src/main are free to import
// node:fs, node:path and friends at the top level, so a renderer file may borrow
// their TYPES (erased at compile time) but never their VALUES: a value import
// pulls the whole module, and its Node built-ins, into the browser bundle, and
// the renderer fails to boot with every locator resolving to nothing.
//
// tsc does not catch this and neither does any unit test, because both run under
// Node. It only shows up in the packaged smoke run, which is a slow and confusing
// place to learn it. Shared values belong in src/shared.
const RENDERER_DIR = path.join(__dirname);

function sourceFiles(dir: string): string[] {
	return readdirSync(dir).flatMap((name) => {
		const full = path.join(dir, name);
		if (statSync(full).isDirectory()) return sourceFiles(full);
		return /\.tsx?$/.test(name) ? [full] : [];
	});
}

// One import statement at a time. The clause may span lines but can never
// contain another `import` or a `from`, which is what keeps this from greedily
// swallowing every preceding import in the file.
const MAIN_IMPORT = /\bimport\s+(type\s+)?((?:(?!\bimport\b|\bfrom\b)[\s\S])*?)\s*from\s+["'](?:\.\.\/)+main\/([^"']+)["']/g;

describe("renderer module boundary", () => {
	it("never imports a value from the main process", () => {
		const offenders: string[] = [];
		for (const file of sourceFiles(RENDERER_DIR)) {
			const source = readFileSync(file, "utf8");
			for (const match of source.matchAll(MAIN_IMPORT)) {
				const [, typeOnlyKeyword, clause, module] = match;
				if (typeOnlyKeyword) continue;
				// A braced clause may still be entirely type-only: { type A, type B }.
				const named = clause.trim().replace(/^\{|\}$/g, "").split(",").map((s) => s.trim()).filter(Boolean);
				const values = named.filter((n) => !n.startsWith("type "));
				if (clause.trim().startsWith("{") && values.length === 0) continue;
				offenders.push(`${path.relative(RENDERER_DIR, file)} imports ${values.join(", ") || clause} from main/${module}`);
			}
		}
		expect(offenders).toEqual([]);
	});
});
