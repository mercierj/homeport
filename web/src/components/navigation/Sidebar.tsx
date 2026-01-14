import { useEffect } from 'react';
import { Link } from 'react-router-dom';
import {
  Home,
  ArrowRightLeft,
  Rocket,
  Database,
  HardDrive,
  Server,
  ListTree,
  ArchiveRestore,
  Code2,
  Terminal,
  Layers,
  Shield,
  ShieldCheck,
  Key,
  Users,
  Globe,
  FileText,
  Activity,
  PanelLeftClose,
  PanelLeft,
  Sun,
  Moon,
} from 'lucide-react';
import { cn } from '../../lib/utils';
import { useSidebarStore } from '../../stores/sidebar';
import { useThemeStore } from '../../stores/theme';
import { SidebarItem } from './SidebarItem';
import { SidebarSection } from './SidebarSection';

export function Sidebar() {
  const { isCollapsed, toggle, setCollapsed } = useSidebarStore();
  const { theme, setTheme } = useThemeStore();

  const toggleTheme = () => {
    setTheme(theme === 'dark' ? 'light' : 'dark');
  };

  // Handle keyboard shortcut (Cmd/Ctrl + B)
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'b') {
        e.preventDefault();
        toggle();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [toggle]);

  // Auto-collapse on mobile
  useEffect(() => {
    const handleResize = () => {
      if (window.innerWidth < 768) {
        setCollapsed(true);
      }
    };
    handleResize();
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, [setCollapsed]);

  return (
    <aside
      className={cn(
        'h-screen bg-sidebar flex flex-col transition-all duration-200 ease-in-out shrink-0 shadow-lg',
        isCollapsed ? 'w-16' : 'w-60'
      )}
    >
      {/* Header */}
      <div className="h-14 px-3 flex items-center justify-between border-b border-sidebar-muted shrink-0">
        <Link
          to="/"
          className={cn(
            'font-bold text-lg text-white transition-opacity duration-150 tracking-tight',
            isCollapsed ? 'opacity-0 w-0 overflow-hidden' : 'opacity-100'
          )}
        >
          Homeport
        </Link>
        <button
          onClick={toggle}
          className={cn(
            'p-2 rounded-md text-sidebar-foreground hover:bg-sidebar-muted hover:text-white transition-colors',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sidebar-active',
            isCollapsed && 'mx-auto'
          )}
          aria-label={isCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          title={isCollapsed ? 'Expand (Cmd+B)' : 'Collapse (Cmd+B)'}
        >
          {isCollapsed ? <PanelLeft className="h-4 w-4" /> : <PanelLeftClose className="h-4 w-4" />}
        </button>
      </div>

      {/* Navigation */}
      <nav className="flex-1 overflow-y-auto p-3 space-y-6">
        {/* Dashboard - Always at top */}
        <div className="space-y-0.5">
          <SidebarItem icon={Home} href="/" label="Dashboard" />
        </div>

        {/* Migration - Hero feature */}
        <div className="space-y-0.5">
          <SidebarItem icon={ArrowRightLeft} href="/migrate" label="Migrate" variant="primary" />
        </div>

        {/* Compute & Functions */}
        <SidebarSection label="Compute">
          <SidebarItem icon={Rocket} href="/deploy" label="Deploy" />
          <SidebarItem icon={Code2} href="/functions" label="Functions" />
          <SidebarItem icon={Terminal} href="/terminal" label="Terminal" />
          <SidebarItem icon={Layers} href="/stacks" label="Stacks" />
        </SidebarSection>

        {/* Data & Storage */}
        <SidebarSection label="Data & Storage">
          <SidebarItem icon={Database} href="/database" label="Database" />
          <SidebarItem icon={HardDrive} href="/storage" label="Storage" />
          <SidebarItem icon={Server} href="/cache" label="Cache" />
          <SidebarItem icon={ListTree} href="/queues" label="Queues" />
          <SidebarItem icon={ArchiveRestore} href="/backup" label="Backup" />
        </SidebarSection>

        {/* Security & Identity */}
        <SidebarSection label="Security">
          <SidebarItem icon={ShieldCheck} href="/policies" label="Policies" />
          <SidebarItem icon={Shield} href="/certificates" label="Certificates" />
          <SidebarItem icon={Key} href="/secrets" label="Secrets" />
          <SidebarItem icon={Users} href="/identity" label="Identity" />
        </SidebarSection>

        {/* Networking */}
        <SidebarSection label="Networking">
          <SidebarItem icon={Globe} href="/dns" label="DNS" />
        </SidebarSection>

        {/* Observability */}
        <SidebarSection label="Observability">
          <SidebarItem icon={FileText} href="/logs" label="Logs" />
          <SidebarItem icon={Activity} href="/metrics" label="Metrics" />
        </SidebarSection>
      </nav>

      {/* Footer with theme toggle */}
      <div className={cn(
        "border-t border-sidebar-muted shrink-0",
        isCollapsed ? "p-2" : "p-3"
      )}>
        <button
          onClick={toggleTheme}
          className={cn(
            "w-full flex items-center gap-3 px-3 py-2 rounded-md text-sm font-medium transition-all duration-150",
            "text-sidebar-foreground hover:bg-sidebar-muted hover:text-white",
            isCollapsed && "justify-center px-2"
          )}
          title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
        >
          {theme === 'dark' ? (
            <Sun className="h-4 w-4 shrink-0" />
          ) : (
            <Moon className="h-4 w-4 shrink-0" />
          )}
          {!isCollapsed && (
            <span className="truncate">
              {theme === 'dark' ? 'Light Mode' : 'Dark Mode'}
            </span>
          )}
        </button>
        {!isCollapsed && (
          <p className="text-[10px] text-sidebar-foreground/40 text-center mt-2">
            <kbd className="px-1 py-0.5 rounded bg-sidebar-muted text-sidebar-foreground/60 font-mono">⌘B</kbd>
            {' '}collapse
          </p>
        )}
      </div>
    </aside>
  );
}
