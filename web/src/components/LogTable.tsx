import { useLayoutEffect, useRef } from 'react'

export interface Column<T> {
  header: string
  accessor: (entry: T) => string
  render: (entry: T, index: number) => React.ReactNode
  width?: string
}

interface LogTableProps<T> {
  columns: Column<T>[]
  entries: T[]
  keyFn: (entry: T, index: number) => string
  isLoading: boolean
  emptyMessage: string
  loadingMessage: string
  scrollable?: boolean
  maxHeight?: string
}

export function LogTable<T>({
  columns,
  entries,
  keyFn,
  isLoading,
  emptyMessage,
  loadingMessage,
  scrollable = true,
  maxHeight = '28rem',
}: LogTableProps<T>) {
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const anchorRef = useRef<{ key: string; offset: number } | null>(null)
  const followingRef = useRef(true)

  function captureScrollAnchor() {
    const container = scrollRef.current
    if (!container) return
    followingRef.current = container.scrollTop <= 4
    if (followingRef.current) {
      anchorRef.current = null
      return
    }
    const rows = Array.from(container.querySelectorAll<HTMLElement>('[data-log-row="true"]'))
    const firstVisible = rows.find((row) => row.offsetTop + row.offsetHeight > container.scrollTop)
    if (!firstVisible) return
    anchorRef.current = {
      key: firstVisible.dataset.logKey ?? '',
      offset: firstVisible.offsetTop - container.scrollTop,
    }
  }

  useLayoutEffect(() => {
    const container = scrollRef.current
    if (!container) return
    if (followingRef.current) {
      container.scrollTop = 0
      return
    }
    const anchor = anchorRef.current
    if (!anchor?.key) return
    const row = container.querySelector<HTMLElement>(`[data-log-key="${CSS.escape(anchor.key)}"]`)
    if (!row) return
    container.scrollTop = row.offsetTop - anchor.offset
  }, [entries])

  const table = (
    <table className="w-full text-xs">
      <thead className="sticky top-0 z-10 bg-white">
        <tr className="border-b border-[#e2ddd7]">
          {columns.map((col) => (
            <th
              key={col.header}
              className={`px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest${col.width ? ` w-${col.width}` : ''}`}
              style={col.width ? { width: col.width } : undefined}
            >
              {col.header}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {entries.map((entry, index) => {
          const key = keyFn(entry, index)
          return (
            <tr key={key} data-log-row="true" data-log-key={key} className="border-b border-[#f7f5f0] hover:bg-[#faf9f7]">
              {columns.map((col) => (
                <td key={col.header} className="px-3 py-2.5">
                  {col.render(entry, index)}
                </td>
              ))}
            </tr>
          )
        })}
        {!isLoading && entries.length === 0 && (
          <tr>
            <td colSpan={columns.length} className="px-4 py-8 text-center text-[11px] text-stone-400">
              {emptyMessage}
            </td>
          </tr>
        )}
        {isLoading && (
          <tr>
            <td colSpan={columns.length} className="px-4 py-8 text-center text-[11px] text-stone-400">
              {loadingMessage}
            </td>
          </tr>
        )}
      </tbody>
    </table>
  )

  if (!scrollable) {
    return table
  }

  return (
    <div
      ref={scrollRef}
      onScroll={captureScrollAnchor}
      className="overflow-auto log-scroll"
      style={{ maxHeight }}
    >
      {table}
    </div>
  )
}
