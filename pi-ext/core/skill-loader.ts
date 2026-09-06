import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

// Skill loader: exposes the extension's bundled skills/ directory via
// resources_discover. Directory paths are discovered recursively — every
// subdirectory containing a SKILL.md becomes a skill. Skills ship with the
// extension instead of living in ~/.pi/agent/skills, so they reload with
// /reload and version with the repo.
export function registerSkillLoader(pi: ExtensionAPI) {
	const baseDir = dirname(fileURLToPath(import.meta.url));

	pi.on("resources_discover", () => {
		return {
			skillPaths: [join(baseDir, "..", "skills")],
		};
	});
}
