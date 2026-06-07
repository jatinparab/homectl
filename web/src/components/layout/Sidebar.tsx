import styled from '@emotion/styled';
import {
  LayoutDashboard,
  Archive,
  Database,
  Boxes,
  Route,
  ScrollText,
  ShieldCheck,
  LogOut,
} from 'lucide-react';
import { IconButton } from '../ui';
import type { LucideIcon } from 'lucide-react';
import type { Tab } from '../../app/navigation';
import { pathFromTab } from '../../app/navigation';
import { HomectlHeaderLogo } from './HomectlLogo';
import { LiveStatusDot } from './LiveStatusDot';
import { Tooltip } from '../ui/Tooltip';

interface NavItem {
  id: Tab;
  label: string;
  icon: LucideIcon;
}

const navItems: NavItem[] = [
  { id: 'dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { id: 'backups', label: 'Backups', icon: Archive },
  { id: 'storage', label: 'Storage', icon: Database },
  { id: 'containers', label: 'Containers', icon: Boxes },
  { id: 'routing', label: 'Routing', icon: Route },
  { id: 'logs', label: 'Logs', icon: ScrollText },
  { id: 'crowdsec', label: 'CrowdSec', icon: ShieldCheck },
];

/* ── Styled components ── */

const SidebarAside = styled.aside`
  position: fixed;
  top: 0;
  left: 0;
  height: 100dvh;
  z-index: 40;
  display: flex;
  flex-direction: column;
  width: 80px;
  border-right: 1px solid ${({ theme }) => theme.colors.neutralBorder};
  background: ${({ theme }) => theme.colors.neutralSurface};

  @media (min-width: 768px) {
    width: 192px;
  }
`;

const BrandRow = styled.div`
  display: flex;
  height: 56px;
  align-items: center;
  justify-content: center;
  border-bottom: 1px solid ${({ theme }) => theme.colors.neutralBorder};
  padding: 0 12px;

  @media (min-width: 768px) {
    justify-content: flex-start;
    padding: 0 16px;
  }
`;

const SidebarLogo = styled(HomectlHeaderLogo)`
  .homectl-logo-wordmark {
    display: none;
  }

  @media (min-width: 768px) {
    .homectl-logo-wordmark {
      display: grid;
    }
  }
`;

const Nav = styled.nav`
  flex: 1;
  overflow-y: auto;
  padding: 12px 4px;

  @media (min-width: 768px) {
    padding: 12px 8px;
  }
`;

const NavList = styled.div`
  display: flex;
  flex-direction: column;
  gap: 2px;
`;

const NavLink = styled.a<{ $active: boolean }>`
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 8px 12px;
  border-radius: ${({ theme }) => theme.radii.lg};
  font-size: 14px;
  font-weight: 500;
  color: ${({ $active, theme }) =>
    $active ? theme.colors.accentPrimary : theme.colors.textMuted};
  background: ${({ $active, theme }) =>
    $active ? theme.colors.accentSubtle : 'transparent'};
  transition: all 150ms ease;
  cursor: pointer;
  text-decoration: none;

  &:hover {
    background: ${({ $active, theme }) =>
      $active ? theme.colors.accentSubtle : theme.colors.neutralSurfaceAlt};
    color: ${({ $active, theme }) =>
      $active ? theme.colors.accentPrimary : theme.colors.textSecondary};
  }

  @media (min-width: 768px) {
    justify-content: flex-start;
  }
`;

const ActiveIndicator = styled.span<{ $active: boolean }>`
  position: absolute;
  left: 0;
  display: none;
  height: 20px;
  width: 2px;
  border-radius: 0 2px 2px 0;
  background: ${({ $active, theme }) =>
    $active ? theme.colors.accentPrimary : 'transparent'};

  @media (min-width: 768px) {
    display: block;
  }
`;

const NavIcon = styled.span`
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
`;

const NavLabel = styled.span`
  display: none;
  font-size: 14px;
  font-weight: 500;

  @media (min-width: 768px) {
    display: block;
  }
`;

const StatusRow = styled.div`
  border-top: 1px solid ${({ theme }) => theme.colors.neutralBorder};
  padding: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;

  @media (min-width: 768px) {
    padding: 12px 16px;
    justify-content: space-between;
  }
`;



/* ── Component ── */

interface SidebarProps {
  activeTab: Tab;
  onTabChange: (tab: Tab) => void;
  onLogout?: () => void;
}

export function Sidebar({ activeTab, onTabChange, onLogout }: SidebarProps) {
  return (
    <SidebarAside>
      <BrandRow>
        <SidebarLogo markSize={32} />
      </BrandRow>

      <Nav aria-label="Primary navigation">
        <NavList>
          {navItems.map((item) => {
            const Icon = item.icon;
            const active = activeTab === item.id;
            return (
              <NavLink
                key={item.id}
                href={pathFromTab(item.id)}
                title={item.label}
                aria-current={active ? 'page' : undefined}
                $active={active}
                onClick={(e) => {
                  e.preventDefault();
                  onTabChange(item.id);
                }}
              >
                <ActiveIndicator $active={active} />
                <NavIcon>
                  <Icon size={20} strokeWidth={1.8} />
                </NavIcon>
                <NavLabel>{item.label}</NavLabel>
              </NavLink>
            );
          })}
        </NavList>
      </Nav>

      <StatusRow>
        <LiveStatusDot />
        {onLogout && (
          <Tooltip content="Logout">
            <IconButton
              label="Logout"
              variant="ghost"
              color="gray"
              size="2"
              onClick={onLogout}
            >
              <LogOut size={18} strokeWidth={1.8} />
            </IconButton>
          </Tooltip>
        )}
      </StatusRow>
    </SidebarAside>
  );
}
