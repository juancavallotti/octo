import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // Coding standards: keep files small and focused. See docs/coding-standards.md.
  {
    rules: {
      "max-lines": [
        "warn",
        { max: 200, skipBlankLines: true, skipComments: true },
      ],
    },
  },
  // One way to the orchestrator.
  //
  // Its address and the caller's credential belong together, and the only module
  // that holds either is app/actions/client/http.ts. Three places had grown their
  // own copy of the base URL by the time the API started requiring a token, and
  // each of them became a call that worked until it didn't — a pod log panel that
  // was empty for no visible reason, a chat launcher that never appeared.
  //
  // Reading the variable is how that starts, so that is what this refuses.
  {
    files: ["app/**/*.{ts,tsx}"],
    // The client itself, and the tests that set the variable to stand it up.
    ignores: ["app/actions/client/http.ts", "**/*.test.{ts,tsx}"],
    rules: {
      "no-restricted-syntax": [
        "error",
        {
          selector:
            "MemberExpression[object.object.name='process'][object.property.name='env'][property.name='ORCHESTRATOR_URL']",
          message:
            "Reach the orchestrator through app/actions/client/http.ts, which attaches the caller's credential. It is the only module that may read ORCHESTRATOR_URL.",
        },
      ],
    },
  },
  // Tests may be longer than implementation files.
  {
    files: ["**/*.test.{ts,tsx}", "vitest.setup.ts"],
    rules: {
      "max-lines": "off",
    },
  },
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
  ]),
]);

export default eslintConfig;
