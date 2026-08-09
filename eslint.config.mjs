import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";
import { defineConfig, globalIgnores } from "eslint/config";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  {
    rules: {
      "max-len": ["error", { code: 120 }],
      "max-lines": ["error", { max: 999, skipBlankLines: true, skipComments: true }],
      complexity: ["error", { max: 15 }],
      "max-depth": ["error", 4],
      "no-console": ["error", { allow: ["warn", "error"] }],
    },
  },
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    ".vinext/**",
    "dist/**",
    "out/**",
    "coverage/**",
    "services/api/**",
    "next-env.d.ts",
  ]),
]);

export default eslintConfig;
