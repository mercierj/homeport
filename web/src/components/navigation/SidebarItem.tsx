import { Link, useLocation } from 'react-router-dom';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/utils';
import { useSidebarStore } from '../../stores/sidebar';

interface SidebarItemProps {
  icon: LucideIcon;
  label: string;
  href: string;
  variant?: 'default' | 'primary';
}

export function SidebarItem({ icon: Icon, label, href, variant = 'default' }: SidebarItemProps) {
  const location = useLocation();
  const isCollapsed = useSidebarStore((state) => state.isCollapsed);
  const isActive = location.pathname === href;

  return (
    <Link
      to={href}
      className={cn(
        'flex items-center gap-3 px-3 py-2.5 rounded-md text-sm font-medium transition-all duration-150',
        'hover:bg-sidebar-muted hover:text-white',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sidebar-active focus-visible:ring-inset',
        isActive && 'bg-sidebar-active text-white shadow-sm',
        !isActive && 'text-sidebar-foreground',
        variant === 'primary' && !isActive && 'text-sidebar-active',
        isCollapsed && 'justify-center px-2'
      )}
      title={isCollapsed ? label : undefined}
    >
      <Icon className={cn('h-4 w-4 shrink-0', isActive ? 'text-white' : 'text-sidebar-foreground')} />
      {!isCollapsed && <span className="truncate">{label}</span>}
    </Link>
  );
}
