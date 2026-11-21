import { expect, it, vi } from "vitest"

const sent: string[] = []
vi.mock("@/lib/history", () => ({ addHistory: () => {} }))
vi.mock("@/lib/api", () => ({
  upload: (file: File) => {
    sent.push(file.name)
    let done!: () => void
    const promise = new Promise<unknown>(
      (r) => (done = () => r({ id: file.name, is_burn_on_read: true })),
    )
    setTimeout(done, 5)
    return { promise, abort: () => {} }
  },
}))

const { useUpload } = await import("./upload")
const limits = { maxFiles: 10, maxBytes: 1e6, maxHuman: "1 MB" }
const f = (name: string) => new File(["x"], name, { lastModified: 1 })

it("does not upload files removed while the batch is running", async () => {
  const s = useUpload.getState()
  s.add([f("a.txt"), f("b.txt"), f("c.txt")], limits)
  const run = s.uploadAll({ expiry: "24h", token: "", password: "" })
  s.clear() // "Remove all" while a.txt is in flight
  await run
  expect(sent).toEqual(["a.txt"])
})

it("refuses duplicates, oversize files and anything past the file limit", () => {
  const s = useUpload.getState()
  s.clear()
  const big = new File([new Uint8Array(2e6)], "big.bin")
  expect(s.add([f("a.txt"), f("a.txt"), big], limits)).toMatch(/1 MB/)
  expect(useUpload.getState().items.map((i) => i.file.name)).toEqual(["a.txt"])
  expect(s.add([f("b.txt"), f("c.txt")], { ...limits, maxFiles: 2 })).toMatch(/up to 2/)
  expect(useUpload.getState().items).toHaveLength(2)
})
