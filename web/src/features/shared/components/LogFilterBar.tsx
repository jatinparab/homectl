import styled from '@emotion/styled';
import type { LogFilterParams } from '../../../api/types';

export interface LogFilterProps {
  filters: LogFilterParams;
  onChange: (filters: LogFilterParams) => void;
  onApply: (filters: LogFilterParams) => void;
  onClear: () => void;
  extraControls?: React.ReactNode;
  hostOptions?: string[];
  ipOptions?: string[];
  statusOptions?: string[];
  showHost?: boolean;
  showURI?: boolean;
  showStatus?: boolean;
  showSearch?: boolean;
}

const Wrap = styled.div`display: flex; flex-direction: column; gap: 8px;`;
const Row = styled.div`display: flex; flex-wrap: wrap; align-items: end; gap: 8px;`;
const ExtraRow = styled.div`display: flex; flex-wrap: wrap; align-items: center; gap: 8px;`;
const LabelEl = styled.label`display: block;`;
const LabelText = styled.span`display: block; font-size: 10px; font-weight: 600; color: #a8a29e; text-transform: uppercase; letter-spacing: 0.1em;`;
const fieldStyle = `margin-top: 4px; width: 100%; padding: 4px 6px; font-size: 11px; font-weight: normal; text-transform: none; letter-spacing: normal; border: 1px solid #e2ddd7; border-radius: 3px; background: #fff; color: #57534e; &:focus { outline: none; border-color: #14b8a6; }`;
const StyledSelect = styled.select`${fieldStyle}`;
const StyledInput = styled.input`${fieldStyle} &::placeholder { color: #d6d3d1; }`;
const Width36 = styled.div`width: 9rem;`;
const Width32 = styled.div`width: 8rem;`;
const Width24 = styled.div`width: 6rem;`;
const Width40 = styled.div`width: 10rem;`;
const SubmitBtn = styled.button`height: 30px; padding: 0 12px; font-size: 11px; background: #0d9488; color: #fff; border: none; border-radius: 3px; cursor: pointer; display: flex; align-items: center; gap: 6px; &:hover:not(:disabled) { background: #0f766e; } &:disabled { opacity: 0.5; }`;
const ClearBtn = styled.button`height: 30px; padding: 0 12px; font-size: 11px; border: 1px solid #e2ddd7; color: #78716c; background: none; border-radius: 3px; cursor: pointer; &:hover { border-color: #a8a29e; color: #44403c; }`;

export function LogFilterBar({ filters, onChange, onApply, onClear, extraControls, hostOptions = [], ipOptions = [], statusOptions = [], showHost, showURI, showStatus, showSearch = true }: LogFilterProps) {
  return (
    <Wrap>
      <Row>
        {showHost && <Width36><LabelEl><LabelText>Host</LabelText><StyledSelect value={filters.host ?? ''} onChange={(e) => onChange({ ...filters, host: e.target.value || undefined })}><option value="">All hosts</option>{[...new Set(filters.host && !hostOptions.includes(filters.host) ? [filters.host, ...hostOptions] : hostOptions)].map((o) => <option key={o} value={o}>{o}</option>)}</StyledSelect></LabelEl></Width36>}
        <Width32><LabelEl><LabelText>IP</LabelText><StyledInput list={`lf-ip-${showHost ? 'r' : 's'}`} value={filters.ip ?? ''} placeholder="All IPs" onChange={(e) => onChange({ ...filters, ip: e.target.value || undefined })} /><datalist id={`lf-ip-${showHost ? 'r' : 's'}`}>{ipOptions.map((o) => <option key={o} value={o} />)}</datalist></LabelEl></Width32>
        {showURI && <Width36><LabelEl><LabelText>URL</LabelText><StyledInput value={filters.uri ?? ''} placeholder="Filter URL" onChange={(e) => onChange({ ...filters, uri: e.target.value || undefined })} /></LabelEl></Width36>}
        {showStatus && <Width24><LabelEl><LabelText>Status</LabelText><StyledSelect value={filters.status ?? ''} onChange={(e) => onChange({ ...filters, status: e.target.value || undefined })}><option value="">All</option>{[...new Set(filters.status && !statusOptions.includes(filters.status) ? [filters.status, ...statusOptions] : statusOptions)].map((o) => <option key={o} value={o}>{o}</option>)}</StyledSelect></LabelEl></Width24>}
        {showSearch && <Width40><LabelEl><LabelText>Search</LabelText><StyledInput value={filters.search ?? ''} placeholder="Search all fields" onChange={(e) => onChange({ ...filters, search: e.target.value || undefined })} /></LabelEl></Width40>}
        <SubmitBtn type="button" onClick={() => onApply({ ...filters })}>Apply filter</SubmitBtn>
        <ClearBtn type="button" onClick={onClear}>Clear</ClearBtn>
      </Row>
      {extraControls && <ExtraRow>{extraControls}</ExtraRow>}
    </Wrap>
  );
}
