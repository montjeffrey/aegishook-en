import { copyFile, cp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
const root = fileURLToPath(new URL("../", import.meta.url));
await copyFile(join(root, "scripts/hook.sh"), join(root, "internal/assets/hooks.sh"));
await copyFile(join(root, "scripts/hook.ps1"), join(root, "internal/assets/hooks.ps1"));
const ui = join(root, "internal/assets/ui");
// placeholder.txt is tracked: put back the bytes that were checked out (line endings
// included) so a build leaves the working tree clean.
const placeholder = await readFile(join(ui, "placeholder.txt")).catch(
  () => "Generated frontend assets: run npm run build before starting the server.\n",
);
await rm(ui, { recursive: true, force: true });
await mkdir(ui, { recursive: true });
await cp(join(root, "web/dist"), ui, { recursive: true });
await writeFile(join(ui, "placeholder.txt"), placeholder);
await copyFile(
  join(root, "adapter/index.ts"),
  join(root, "internal/assets/adapter.ts"),
);
await copyFile(
  join(root, "adapter/opencode.mjs"),
  join(root, "internal/assets/opencode.mjs"),
);
