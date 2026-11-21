import { expect, it } from "vitest"
import { expiryError, formatDuration, parseExpiry } from "./expiry"

it("parses single and compound durations", () => {
  expect(parseExpiry("90m")).toBe(5400)
  expect(parseExpiry("7d")).toBe(604800)
  expect(parseExpiry(" 2W ")).toBe(1209600)
  expect(parseExpiry("1d12h")).toBe(129600)
})

it("rejects anything the server would", () => {
  for (const bad of ["", "abc", "5", "-5h", "5 d", "1.5h", "h5"])
    expect(parseExpiry(bad)).toBeNull()
})

it("checks the minimum and the server's cap", () => {
  const month = 30 * 86400
  expect(expiryError("5d", month)).toBe("")
  expect(expiryError("30s", month)).toMatch(/1 minute/)
  expect(expiryError("31d", month)).toBe("At most 30 days")
  expect(expiryError("52w", 0)).toBe("") // 0 = unlimited
  expect(expiryError("nope", month)).toMatch(/unit/)
})

it("names durations", () => {
  expect(formatDuration(86400)).toBe("1 day")
  expect(formatDuration(30 * 86400)).toBe("30 days")
  expect(formatDuration(14 * 86400)).toBe("2 weeks")
  expect(formatDuration(36 * 3600)).toBe("36 hours")
})
