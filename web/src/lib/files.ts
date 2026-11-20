const UNITS = ["B", "KB", "MB", "GB", "TB"];

/** 1536 -> "1.5 KB". Clamped so values under 1 byte or NaN don't index off either end of UNITS. */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const i = Math.min(
    Math.max(Math.floor(Math.log(bytes) / Math.log(1024)), 0),
    UNITS.length - 1,
  );
  return `${parseFloat((bytes / 1024 ** i).toFixed(1))} ${UNITS[i]}`;
}

const EXT_TYPE: [RegExp, string][] = [
  [/\.(svg|png|jpe?g|gif|webp|avif|heic|heif|bmp|ico|tiff?)$/i, "image/"],
  [/\.(mp4|mov|mkv|webm|avi|m4v|wmv)$/i, "video/"],
  [/\.(mp3|wav|flac|ogg|m4a|aac|opus)$/i, "audio/"],
];

const mimeOf = (name: string, type: string) =>
  type || EXT_TYPE.find(([re]) => re.test(name))?.[1] || "";

export const isImage = (name: string, type = "") =>
  mimeOf(name, type).startsWith("image/");

/** Short document type for the description line: "PDF", "ZIP", "JPG"... */
export function fileKind(name: string, type = ""): string {
  const ext = name.match(/\.([a-z0-9]{1,5})$/i)?.[1]?.toLowerCase();
  const k = ext ?? type.split("/")[1]?.split(/[+;]/)[0] ?? "file";
  return (k === "jpeg" ? "jpg" : k).toUpperCase();
}

/** First match wins. Values are `data-icon` keys in IconTemplates (ported from warpdrop's file-icon.tsx). */
const BY_EXT: [RegExp, string][] = [
  [/\.svg$/i, "file-type-svg"],
  [/\.pdf$/i, "file-type-pdf"],
  [/\.(docx?|odt|rtf)$/i, "file-type-doc"],
  [/\.csv$/i, "file-type-csv"],
  [/\.(xlsx?|ods)$/i, "file-type-xls"],
  [/\.(numbers|tsv)$/i, "file-spreadsheet"],
  [/\.(pptx?|odp|key)$/i, "file-type-ppt"],
  [/\.(zip|rar|7z|tar|gz|tgz|xz|bz2|zst)$/i, "file-type-zip"],
  [/\.(iso|dmg|img)$/i, "disc"],
  [/\.(apk|deb|rpm|pkg|msi|appimage|exe)$/i, "package"],
  [/\.(txt|log)$/i, "file-type-txt"],
  [/\.mdx?$/i, "markdown"],
  [/\.(epub|mobi|azw3?)$/i, "book"],
  [/\.tsx$/i, "file-type-tsx"],
  [/\.ts$/i, "file-type-ts"],
  [/\.(jsx?|mjs|cjs)$/i, "file-type-js"],
  [/\.(css|scss)$/i, "file-type-css"],
  [/\.html?$/i, "file-type-html"],
  [/\.sql$/i, "file-type-sql"],
  [/\.xml$/i, "file-type-xml"],
  [/\.(json|ya?ml|toml|ini|env)$/i, "braces"],
  [/\.(sh|bash|zsh|fish|bat|ps1)$/i, "terminal-2"],
  [
    /\.(py|go|rs|c|h|cc|cpp|hpp|java|kt|swift|rb|php|lua|vue|svelte|dart|cs)$/i,
    "file-code",
  ],
  [/\.(db|sqlite3?|sql3)$/i, "database"],
  [/\.(pem|crt|cer|csr)$/i, "certificate"],
  [/\.(gpg|asc|pub|ppk)$/i, "key"],
  [/\.(ttf|otf|woff2?)$/i, "file-typography"],
  [/\.(bin|dat|o|so|dll|wasm)$/i, "binary"],
];

/** Key of the matching `data-icon` in IconTemplates: by extension first, then MIME type. */
export function fileIconKey(name: string, type = ""): string {
  const byExt = BY_EXT.find(([re]) => re.test(name))?.[1];
  if (byExt) return byExt;
  const mime = mimeOf(name, type);
  if (mime.startsWith("image/")) return "photo";
  if (mime.startsWith("video/")) return "movie";
  if (mime.startsWith("audio/")) return "file-music";
  if (mime.startsWith("text/")) return "file-text";
  return "file";
}

/** Fresh copy of a pre-rendered icon from IconTemplates (Astro components can't be rendered client-side). */
export function cloneIcon(name: string): HTMLElement {
  const icon = document.querySelector(
    `[data-icons] [data-icon="${name}"]`,
  )?.firstElementChild;
  return (
    (icon?.cloneNode(true) as HTMLElement) ?? document.createElement("span")
  );
}
