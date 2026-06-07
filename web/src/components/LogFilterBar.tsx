import type { LogFilterParams } from '../api/types'

export interface LogFilterProps {
  filters: LogFilterParams
  onChange: (filters: LogFilterParams) => void
  onApply: (filters: LogFilterParams) => void
  onClear: () => void
  extraControls?: React.ReactNode
  hostOptions?: string[]
  ipOptions?: string[]
  statusOptions?: string[]
  showHost?: boolean
  showURI?: boolean
  showStatus?: boolean
  showSearch?: boolean
}

function FilterSelect({
  label,
  value,
  options,
  allLabel,
  onChange,
}: {
  label: string
  value: string
  options: string[]
  allLabel: string
  onChange: (value: string) => void
}) {
  const visibleOptions = value && !options.includes(value) ? [value, ...options] : options
  return (
    <label className="block">
      <span className="block text-[10px] font-semibold text-stone-400 uppercase tracking-widest">{label}</span>
      <select
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="mt-1 w-full px-1.5 py-1 text-[11px] normal-case tracking-normal font-normal border border-[#e2ddd7] rounded bg-white text-stone-600 focus:outline-none focus:border-teal-500"
      >
        <option value="">{allLabel}</option>
        {visibleOptions.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </select>
    </label>
  )
}

function FilterSearchInput({
  label,
  value,
  options,
  placeholder,
  listId,
  onChange,
}: {
  label: string
  value: string
  options: string[]
  placeholder: string
  listId: string
  onChange: (value: string) => void
}) {
  return (
    <label className="block">
      <span className="block text-[10px] font-semibold text-stone-400 uppercase tracking-widest">{label}</span>
      <input
        list={listId}
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        className="mt-1 w-full px-1.5 py-1 text-[11px] normal-case tracking-normal font-normal border border-[#e2ddd7] rounded bg-white text-stone-600 placeholder:text-stone-300 focus:outline-none focus:border-teal-500"
      />
      <datalist id={listId}>
        {options.map((option) => (
          <option key={option} value={option} />
        ))}
      </datalist>
    </label>
  )
}

function FilterTextInput({
  label,
  value,
  placeholder,
  onChange,
}: {
  label: string
  value: string
  placeholder: string
  onChange: (value: string) => void
}) {
  return (
    <label className="block">
      <span className="block text-[10px] font-semibold text-stone-400 uppercase tracking-widest">{label}</span>
      <input
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        className="mt-1 w-full px-1.5 py-1 text-[11px] normal-case tracking-normal font-normal border border-[#e2ddd7] rounded bg-white text-stone-600 placeholder:text-stone-300 focus:outline-none focus:border-teal-500"
      />
    </label>
  )
}

export function LogFilterBar({
  filters,
  onChange,
  onApply,
  extraControls,
  hostOptions = [],
  ipOptions = [],
  statusOptions = [],
  showHost = false,
  showURI = false,
  showStatus = false,
  showSearch = true,
  onClear,
}: LogFilterProps) {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-end gap-2">
        {showHost && (
          <div className="w-36">
            <FilterSelect
              label="Host"
              value={filters.host ?? ''}
              options={hostOptions}
              allLabel="All hosts"
              onChange={(host) => onChange({ ...filters, host: host || undefined })}
            />
          </div>
        )}
        <div className="w-32">
          <FilterSearchInput
            label="IP"
            value={filters.ip ?? ''}
            options={ipOptions}
            placeholder="All IPs"
            listId={`log-filter-ip-options-${showHost ? 'request' : 'ssh'}`}
            onChange={(ip) => onChange({ ...filters, ip: ip || undefined })}
          />
        </div>
        {showURI && (
          <div className="w-36">
            <FilterTextInput
              label="URL"
              value={filters.uri ?? ''}
              placeholder="Filter URL"
              onChange={(uri) => onChange({ ...filters, uri: uri || undefined })}
            />
          </div>
        )}
        {showStatus && (
          <div className="w-24">
            <FilterSelect
              label="Status"
              value={filters.status ?? ''}
              options={statusOptions}
              allLabel="All"
              onChange={(status) => onChange({ ...filters, status: status || undefined })}
            />
          </div>
        )}
        {showSearch && (
          <div className="w-40">
            <FilterTextInput
              label="Search"
              value={filters.search ?? ''}
              placeholder="Search all fields"
              onChange={(search) => onChange({ ...filters, search: search || undefined })}
            />
          </div>
        )}
        <button
          type="button"
          onClick={() => onApply({ ...filters })}
          className="h-[30px] px-3 text-[11px] bg-[#0d9488] text-white rounded hover:bg-[#0f766e] disabled:opacity-50 flex items-center gap-1.5"
        >
          Apply filter
        </button>
        <button
          type="button"
          onClick={onClear}
          className="h-[30px] px-3 text-[11px] border border-[#e2ddd7] text-stone-500 hover:border-stone-400 hover:text-stone-700 rounded"
        >
          Clear
        </button>
      </div>
      {extraControls && (
        <div className="flex flex-wrap items-center gap-2">{extraControls}</div>
      )}
    </div>
  )
}
