/** @type {import("prettier").Config} */
export default {
  semi: true,
  singleQuote: false,
  trailingComma: "all",
  plugins: ["prettier-plugin-astro", "prettier-plugin-tailwindcss"],
  overrides: [
    {
      files: "*.astro",
      options: {
        parser: "astro",
      },
    },
  ],
};
