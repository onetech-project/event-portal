// Generates the backend's icon catalog file from the installed lucide-react.
//
// The file is the single source of truth binding the admin icon picker to
// server-side validation (spec 009, contracts/content-icon-contract.md §3):
// every key the picker can offer must be accepted by the backend's
// validateIcon, which embeds this file via go:embed.
//
// Re-run whenever the lucide-react version changes, and commit the result in
// the same change:
//
//   cd frontend && bun scripts/generate-icon-keys.mjs   (or: node scripts/...)
//
// The catalog-sync vitest test (generate-icon-keys.test.ts) fails until the
// checked-in file matches the installed package again.

import { writeFileSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { dynamicIconImports } from "lucide-react/dynamic";

const outFile = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../backend/internal/event/iconkeys/icon_keys.txt",
);

const keys = Object.keys(dynamicIconImports).sort();

const malformed = keys.filter((k) => !/^[a-z0-9]+(-[a-z0-9]+)*$/.test(k));
if (malformed.length > 0) {
  throw new Error(`keys violating the contract's slug format: ${malformed.join(", ")}`);
}

mkdirSync(dirname(outFile), { recursive: true });
writeFileSync(outFile, keys.join("\n") + "\n");
console.log(`wrote ${keys.length} icon keys to ${outFile}`);
