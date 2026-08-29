import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["node_modules/", "dist/", "web/"] },
  {
    files: ["pipeline/**/*.ts", "tasking/**/*.ts"],
    extends: [...tseslint.configs.recommended],
    rules: {
      // Error everywhere in the typed folders; the pre-split legacy `any`
      // occurrences carry explicit per-line eslint-disable markers (the
      // allowlist), so any new unmarked `any` fails lint.
      "@typescript-eslint/no-explicit-any": "error",
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_" }],
    },
  },
);
