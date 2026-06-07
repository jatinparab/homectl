import { useLayoutEffect, useRef } from 'react';
import styled from '@emotion/styled';

export interface Column<T> {
  header: string;
  accessor: (entry: T) => string;
  render: (entry: T, index: number) => React.ReactNode;
  width?: string;
}

interface LogTableProps<T> {
  columns: Column<T>[];
  entries: T[];
  keyFn: (entry: T, index: number) => string;
  isLoading: boolean;
  emptyMessage: string;
  loadingMessage: string;
  scrollable?: boolean;
  maxHeight?: string;
}

const Table = styled.table`width: 100%; font-size: 12px;`;
const Thead = styled.thead`position: sticky; top: 0; z-index: 10; background: #fff;`;
const Th = styled.th<{ $w?: string }>`padding: 10px 12px; text-align: left; font-size: 10px; font-weight: 600; color: #a8a29e; text-transform: uppercase; letter-spacing: 0.1em; ${(p) => p.$w ? `width: ${p.$w};` : ''}`;
const Td = styled.td`padding: 10px 12px;`;
const Tr = styled.tr`border-bottom: 1px solid #f7f5f0; &:hover { background: #faf9f7; }`;
const Empty = styled.td`padding: 32px 16px; text-align: center; font-size: 11px; color: #a8a29e;`;
const Scroll = styled.div<{ $maxH: string }>`overflow: auto; max-height: ${(p) => p.$maxH};`;

export function LogTable<T>({ columns, entries, keyFn, isLoading, emptyMessage, loadingMessage, scrollable = true, maxHeight = '28rem' }: LogTableProps<T>) {
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const anchorRef = useRef<{ key: string; offset: number } | null>(null);
  const followingRef = useRef(true);

  function captureScrollAnchor() {
    const container = scrollRef.current;
    if (!container) return;
    followingRef.current = container.scrollTop <= 4;
    if (followingRef.current) { anchorRef.current = null; return; }
    const rows = Array.from(container.querySelectorAll<HTMLElement>('[data-log-row="true"]'));
    const firstVisible = rows.find((row) => row.offsetTop + row.offsetHeight > container.scrollTop);
    if (!firstVisible) return;
    anchorRef.current = { key: firstVisible.dataset.logKey ?? '', offset: firstVisible.offsetTop - container.scrollTop };
  }
  useLayoutEffect(() => {
    const c = scrollRef.current; if (!c) return;
    if (followingRef.current) { c.scrollTop = 0; return; }
    const a = anchorRef.current; if (!a?.key) return;
    const row = c.querySelector<HTMLElement>(`[data-log-key="${CSS.escape(a.key)}"]`);
    if (!row) return; c.scrollTop = row.offsetTop - a.offset;
  }, [entries]);

  const tbl = (
    <Table>
      <Thead><tr>{columns.map((col) => <Th key={col.header} $w={col.width}>{col.header}</Th>)}</tr></Thead>
      <tbody>
        {entries.map((entry, i) => <Tr key={keyFn(entry, i)} data-log-row="true" data-log-key={keyFn(entry, i)}>{columns.map((col) => <Td key={col.header}>{col.render(entry, i)}</Td>)}</Tr>)}
        {!isLoading && entries.length === 0 && <tr><Empty colSpan={columns.length}>{emptyMessage}</Empty></tr>}
        {isLoading && <tr><Empty colSpan={columns.length}>{loadingMessage}</Empty></tr>}
      </tbody>
    </Table>
  );
  if (!scrollable) return tbl;
  return <Scroll ref={scrollRef} $maxH={maxHeight} onScroll={captureScrollAnchor}>{tbl}</Scroll>;
}
