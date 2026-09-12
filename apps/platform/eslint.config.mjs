import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

/**
 * Every way a module could read one of the single-entry-point env variables.
 *
 * Dot access is the form somebody writes on purpose; the other two are the forms
 * a rule that only knows about dot access silently permits — and a bypass that
 * lints clean is worse than no rule, because it reads as enforcement.
 */
function noDirectEnvRead(name, through) {
  const message =
    `Reach it through ${through}, which attaches the caller's credential. ` +
    `It is the only module that may read ${name}.`;
  return [
    // process.env.NAME
    {
      selector:
        `MemberExpression[object.object.name='process'][object.property.name='env'][property.name='${name}']`,
      message,
    },
    // process.env["NAME"]
    {
      selector:
        `MemberExpression[computed=true][object.object.name='process'][object.property.name='env'][property.value='${name}']`,
      message,
    },
    // const { NAME } = process.env
    {
      selector:
        `VariableDeclarator[init.object.name='process'][init.property.name='env'] > ObjectPattern > Property[key.name='${name}']`,
      message,
    },
  ];
}

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
  //
  // Both variables are declared in ONE block, and that is load-bearing. Flat
  // config does not merge the options of a rule configured twice — the last
  // block matching a file wins outright — so two blocks each naming
  // `no-restricted-syntax` over `app/**` meant the second silently switched the
  // first off, and the orchestrator rule enforced nothing at all.
  {
    files: ["app/**/*.{ts,tsx}"],
    // Tests set these variables to stand the services up.
    ignores: ["**/*.test.{ts,tsx}"],
    rules: {
      "no-restricted-syntax": [
        "error",
        ...noDirectEnvRead("ORCHESTRATOR_URL", "app/actions/client/http.ts"),
        ...noDirectEnvRead("OBSERVABILITY_URL", "app/actions/_observability.ts"),
      ],
    },
  },
  // The two entry points themselves, each allowed its own variable and no more.
  // Written as an override rather than an `ignores` for the same reason as above:
  // what a later block says about a rule is the whole of what that rule is.
  {
    files: ["app/actions/client/http.ts"],
    rules: {
      "no-restricted-syntax": [
        "error",
        ...noDirectEnvRead("OBSERVABILITY_URL", "app/actions/_observability.ts"),
      ],
    },
  },
  {
    files: ["app/actions/_observability.ts"],
    rules: {
      "no-restricted-syntax": [
        "error",
        ...noDirectEnvRead("ORCHESTRATOR_URL", "app/actions/client/http.ts"),
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
