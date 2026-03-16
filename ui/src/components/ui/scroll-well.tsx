import * as React from 'react'
import { cn } from '@/lib/utils'

interface ScrollWellProps extends React.HTMLAttributes<HTMLDivElement> {
  /** Fixed height in pixels. Defaults to 100. */
  height?: number
}

/**
 * A bordered, fixed-height container that scrolls its content vertically.
 * Use this to constrain lists of badges, tags, or other flowing content
 * so they don't push surrounding UI out of view.
 */
const ScrollWell = React.forwardRef<HTMLDivElement, ScrollWellProps>(
  ({ className, height = 100, style, children, ...props }, ref) => (
    <div
      ref={ref}
      className={cn('rounded-md border border-border p-2 overflow-y-auto', className)}
      style={{ height: `${height}px`, ...style }}
      {...props}
    >
      {children}
    </div>
  ),
)
ScrollWell.displayName = 'ScrollWell'

export { ScrollWell }
