## Development

When starting the dev server, use background mode:

```
astro dev --background
```

Manage the background server with `astro dev stop`, `astro dev status`, and `astro dev logs`.

## Documentation

Full documentation: https://docs.astro.build

Consult these guides before working on related tasks:

- [Adding pages, dynamic routes, or middleware](https://docs.astro.build/en/guides/routing/)
- [Working with Astro components](https://docs.astro.build/en/basics/astro-components/)
- [Using React, Vue, Svelte, or other framework components](https://docs.astro.build/en/guides/framework-components/)
- [Adding or managing content](https://docs.astro.build/en/guides/content-collections/)
- [Adding styles or using Tailwind](https://docs.astro.build/en/guides/styling/)
- [Supporting multiple languages](https://docs.astro.build/en/guides/internationalization/)

## Code Formatting Guidelines

All code in this project must be formatted using Prettier. Do NOT run or suggest Ultracite, Biome, or ESLint.

### Plugins & Rules

- **Astro files (`.astro`)** are formatted via `prettier-plugin-astro`.
- **Tailwind classes** are sorted automatically via `prettier-plugin-tailwindcss`.
- Do not manually rearrange Tailwind classes—let the plugin handle sorting.

### Formatting Commands

Always format files after creating or modifying them using the project scripts:

- **Format the entire project:**

  ```bash
  pnpm run format
  ```

- **Format a specific file (preferred after targeted edits):**

  ```bash
  pnpm exec prettier --write "<path/to/file>"
  ```

- **Verify formatting without modifying files:**
  ```bash
  pnpm run format:check
  ```

### Agent Rules of Engagement

1. Run `pnpm run format` (or format the targeted file directly) before concluding any task.
2. Ensure `.astro` frontmatter fences (`---`) and component imports remain intact after formatting.
3. If formatting fails on an `.astro` file, inspect for unclosed JSX/HTML tags or invalid frontmatter syntax rather than bypassing Prettier.
