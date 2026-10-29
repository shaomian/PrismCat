import * as React from "react"

import { cn } from "@/lib/utils"

function Input({
  className,
  type,
  startIcon,
  ...props
}: React.ComponentProps<"input"> & { startIcon?: React.ReactNode }) {
  const input = (
    <input
      type={type}
      data-slot="input"
      className={cn(
        "file:text-foreground placeholder:text-muted-foreground selection:bg-primary selection:text-primary-foreground dark:bg-input/30 border-input h-8 w-full min-w-0 rounded-md border bg-transparent px-3 py-1 text-base transition-[color,box-shadow] outline-none file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm",
        "focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]",
        "aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive",
        startIcon && "pl-9",
        className
      )}
      {...props}
    />
  )
  if (!startIcon) return input
  return (
    <div className="group relative w-full">
      <span
        aria-hidden="true"
        className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground/75 transition-colors group-focus-within:text-primary [&>svg]:h-4 [&>svg]:w-4"
      >
        {startIcon}
      </span>
      {input}
    </div>
  )
}

export { Input }
