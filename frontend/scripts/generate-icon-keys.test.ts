import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { dynamicIconImports } from "lucide-react/dynamic";
import { describe, expect, it } from "vitest";

// Catalog-sync drift protection (spec 009, contract §3): the checked-in file
// the backend embeds must exactly match the installed lucide-react's dynamic
// catalog. If this fails, run `bun scripts/generate-icon-keys.mjs` and commit
// the regenerated icon_keys.txt together with the lucide-react bump.
describe("icon_keys.txt catalog sync", () => {
  it("matches Object.keys(dynamicIconImports), sorted", () => {
    const file = resolve(
      __dirname,
      "../../backend/internal/event/iconkeys/icon_keys.txt",
    );
    const checkedIn = readFileSync(file, "utf8").trim().split("\n");
    const installed = Object.keys(dynamicIconImports).sort();

    expect(checkedIn).toEqual(installed);
  });
});
