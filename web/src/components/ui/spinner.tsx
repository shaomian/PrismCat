import * as React from "react"

import { cn } from "@/lib/utils"

/**
 * 加载转圈。装饰性元素,语义由旁边的文字承担,所以 aria-hidden。
 * 默认 20px,整页加载场景传 className="h-6 w-6"。
 */
function Spinner({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      aria-hidden="true"
      className={cn(
        "h-5 w-5 animate-spin rounded-full border-2 border-primary border-t-transparent",
        className
      )}
      {...props}
    />
  )
}

export { Spinner }
