import { QR } from "@/components/qr"
import { AttachmentAction } from "@/components/ui/attachment"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { IconQrcode } from "@tabler/icons-react"

/** Attachment action that pops up a QR code of `value`, for opening a link on a phone. */
export function QrAction({ value, label }: { value: string; label: string }) {
  return (
    <Popover>
      <PopoverTrigger render={<AttachmentAction aria-label={label} title={label} />}>
        <IconQrcode />
      </PopoverTrigger>
      <PopoverContent className="w-auto p-2">
        <QR value={value} />
      </PopoverContent>
    </Popover>
  )
}
