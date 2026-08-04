import coreWebVitals from "eslint-config-next/core-web-vitals";
import typescript from "eslint-config-next/typescript";

/** Flat ESLint config for the dashboard. Next 16 ships flat config arrays directly. */
const config = [
  {
    ignores: [".next/**", "node_modules/**", "next-env.d.ts", "reference/**"],
  },
  ...coreWebVitals,
  ...typescript,
];

export default config;
