import { connection } from "next/server";

import { runtimeConfigFromEnv, runtimeConfigScript } from "@/lib/runtime-config";

/**
 * Writes the runtime configuration into the document head, ahead of every
 * application script.
 *
 * `connection()` opts this render out of prerendering. Without it Next would
 * evaluate the component at *build* time and freeze the build machine's
 * environment into the emitted HTML — the exact thing the runtime config exists
 * to avoid. The cost is that pages render per request, which is nearly free
 * here: every page is a client shell and all data is fetched from the browser.
 */
export async function RuntimeConfigScript() {
  await connection();

  return (
    <script
      id="runtime-config"
      // The content is JSON built from our own environment, not user input, and
      // `</script>` is escaped by runtimeConfigScript.
      dangerouslySetInnerHTML={{ __html: runtimeConfigScript(runtimeConfigFromEnv()) }}
    />
  );
}
