// A clean source checkout has no dist. Use the existing input-bound artifact
// preparation before any sidebar consumer reads its generated HTML/assets.
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const owned = !process.env.AICRM_TEST_PREP_DIR;
const prep = process.env.AICRM_TEST_PREP_DIR || fs.mkdtempSync(path.join(os.tmpdir(), "aicrm-sidebar-probe-"));
const env = { ...process.env, AICRM_TEST_PREP_DIR: prep, PYTHONDONTWRITEBYTECODE: "1" };
const run = (command, args) => execFileSync(command, args, { cwd: root, env, stdio: "inherit" });
try {
  run(process.execPath, ["scripts/prepare-donor-source-views.mjs"]);
  run("python3", ["scripts/ci/check_preparation.py", "artifact"]);
  for (const file of ["rpaConfiguration.test.mjs", "sidebarGroupNavigation.test.mjs", "sidebarGroupBindings.test.mjs", "sidebarOneIDPresentation.test.mjs", "sidebarOpenChatProbe.test.mjs", "sidebar_send_recovery.test.mjs"]) {
    console.log("sidebar probe contract:", file);
    run(process.execPath, ["web/v3/" + file]);
  }
} finally {
  if (owned) fs.rmSync(prep, { recursive: true, force: true });
}
