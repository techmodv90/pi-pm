import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["node_modules/", "dist/", "web/"] },
  {
    files: ["pipeline/**/*.ts", "tasking/**/*.ts"],
    extends: [...tseslint.configs.recommended],
    rules: {
      // Burn-down mode: existing modules still carry `any` from the pre-split
      // scheduler; new code must not add more (warn), and the typed boundary
      // forbids it outright (error below).
      "@typescript-eslint/no-explicit-any": "warn",
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_" }],
    },
  },
  {
    files: ["pipeline/pic-show.ts"],
    rules: {
      "@typescript-eslint/no-explicit-any": "error",
    },
  },
);
