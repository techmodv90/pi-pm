import assert from "node:assert/strict";
import { test } from "node:test";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { registerSkillLoader } from "./skill-loader.ts";

test("resources_discover returns the bundled skills directory", () => {
	const handlers: Map<string, Array<() => unknown>> = new Map();
	const pi = {
		on: (event: string, handler: () => unknown) => {
			const list = handlers.get(event) ?? [];
			list.push(handler);
			handlers.set(event, list);
		},
	} as never;
	registerSkillLoader(pi);
	const handler = handlers.get("resources_discover")![0]!;
	const result = handler() as { skillPaths: string[] };
	const expected = join(dirname(fileURLToPath(import.meta.url)), "..", "skills");
	assert.deepEqual(result.skillPaths, [expected]);
});
