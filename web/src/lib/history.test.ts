import { expect, it } from "vitest"
import { parseHistory } from "./history"

const rec = (id: string, expiresAt: string) => ({
  id,
  name: `${id}.txt`,
  size: 1,
  url: `https://x/f/${id}.txt`,
  deleteToken: "t",
  expiresAt,
  createdAt: "2026-01-01T00:00:00Z",
})

it("drops expired and malformed records, keeps the rest in order", () => {
  const now = Date.parse("2026-06-01T00:00:00Z")
  const raw = JSON.stringify([
    rec("live", "2026-06-02T00:00:00Z"),
    rec("dead", "2026-05-31T00:00:00Z"),
    null,
    { id: 3 },
    rec("noexp", ""),
  ])
  expect(parseHistory(raw, now).map((r) => r.id)).toEqual(["live", "noexp"])
})

it("survives garbage in storage", () => {
  expect(parseHistory("{not json")).toEqual([])
  expect(parseHistory('{"a":1}')).toEqual([])
  expect(parseHistory(null)).toEqual([])
})
