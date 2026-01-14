import { useSidebarStore } from '../../stores/sidebar';

interface SidebarSectionProps {
  label: string;
  children: React.ReactNode;
}

export function SidebarSection({ label, children }: SidebarSectionProps) {
  const isCollapsed = useSidebarStore((state) => state.isCollapsed);

  return (
    <div className="space-y-1">
      {!isCollapsed && (
        <h3 className="px-3 text-[11px] font-semibold text-sidebar-foreground/50 uppercase tracking-wider mb-2">
          {label}
        </h3>
      )}
      {isCollapsed && <div className="h-px bg-sidebar-muted mx-2 my-2" />}
      <div className="space-y-0.5">{children}</div>
    </div>
  );
}
