import { useEffect, useRef, useCallback, useState } from 'react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import { Button } from '@/components/ui/button';
import { X, Maximize2, Minimize2, RefreshCw, Loader2 } from 'lucide-react';
import { cn } from '@/lib/utils';
import {
  createTerminalConnection,
  TerminalConnection,
  type ConnectedData,
  type ErrorData,
} from '@/lib/terminal-api';
import type { Container } from '@/lib/docker-api';

import '@xterm/xterm/css/xterm.css';

interface TerminalProps {
  container: Container;
  onClose: () => void;
}

export function Terminal({ container, onClose }: TerminalProps) {
  const terminalRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const connectionRef = useRef<TerminalConnection | null>(null);

  const [isConnected, setIsConnected] = useState(false);
  const [isConnecting, setIsConnecting] = useState(true);
  const [isMaximized, setIsMaximized] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sessionId, setSessionId] = useState<string | null>(null);

  // Initialize terminal
  useEffect(() => {
    if (!terminalRef.current) return;

    const xterm = new XTerm({
      cursorBlink: true,
      cursorStyle: 'block',
      fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace',
      fontSize: 14,
      lineHeight: 1.2,
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
        cursor: '#aeafad',
        cursorAccent: '#1e1e1e',
        selectionBackground: '#264f78',
        black: '#000000',
        red: '#cd3131',
        green: '#0dbc79',
        yellow: '#e5e510',
        blue: '#2472c8',
        magenta: '#bc3fbc',
        cyan: '#11a8cd',
        white: '#e5e5e5',
        brightBlack: '#666666',
        brightRed: '#f14c4c',
        brightGreen: '#23d18b',
        brightYellow: '#f5f543',
        brightBlue: '#3b8eea',
        brightMagenta: '#d670d6',
        brightCyan: '#29b8db',
        brightWhite: '#ffffff',
      },
    });

    const fitAddon = new FitAddon();
    const webLinksAddon = new WebLinksAddon();

    xterm.loadAddon(fitAddon);
    xterm.loadAddon(webLinksAddon);

    xterm.open(terminalRef.current);
    fitAddon.fit();

    xtermRef.current = xterm;
    fitAddonRef.current = fitAddon;

    // Connect to container
    const connection = createTerminalConnection({
      containerID: container.id,
      cols: xterm.cols,
      rows: xterm.rows,
      onOutput: (data) => {
        xterm.write(data);
      },
      onConnected: (data: ConnectedData) => {
        setIsConnected(true);
        setIsConnecting(false);
        setSessionId(data.session_id);
        xterm.write(`\r\n\x1b[32mConnected to ${container.name}\x1b[0m\r\n\r\n`);
      },
      onError: (err: ErrorData) => {
        setError(err.error);
        setIsConnecting(false);
        xterm.write(`\r\n\x1b[31mError: ${err.error}\x1b[0m\r\n`);
      },
      onClose: () => {
        setIsConnected(false);
        xterm.write('\r\n\x1b[33mConnection closed\x1b[0m\r\n');
      },
    });

    connection.connect();
    connectionRef.current = connection;

    // Handle input
    xterm.onData((data) => {
      connection.send(data);
    });

    // Handle resize
    const handleResize = () => {
      if (fitAddonRef.current && xtermRef.current) {
        fitAddonRef.current.fit();
        connection.resize(xtermRef.current.cols, xtermRef.current.rows);
      }
    };

    window.addEventListener('resize', handleResize);

    // Focus terminal
    xterm.focus();

    return () => {
      window.removeEventListener('resize', handleResize);
      connection.disconnect();
      xterm.dispose();
    };
  }, [container.id, container.name]);

  // Handle maximize/minimize
  useEffect(() => {
    if (fitAddonRef.current) {
      // Small delay to allow DOM to update
      setTimeout(() => {
        fitAddonRef.current?.fit();
        if (connectionRef.current && xtermRef.current) {
          connectionRef.current.resize(xtermRef.current.cols, xtermRef.current.rows);
        }
      }, 100);
    }
  }, [isMaximized]);

  const handleReconnect = useCallback(() => {
    if (connectionRef.current) {
      setIsConnecting(true);
      setError(null);
      connectionRef.current.disconnect();
      connectionRef.current.connect();
    }
  }, []);

  const handleClose = useCallback(() => {
    if (connectionRef.current) {
      connectionRef.current.disconnect();
    }
    onClose();
  }, [onClose]);

  const toggleMaximize = useCallback(() => {
    setIsMaximized((prev) => !prev);
  }, []);

  return (
    <div
      className={cn(
        'fixed z-50 flex flex-col bg-background border rounded-lg shadow-xl overflow-hidden',
        isMaximized
          ? 'inset-4'
          : 'inset-x-4 bottom-4 top-1/4 md:inset-auto md:right-4 md:bottom-4 md:w-[800px] md:h-[500px]'
      )}
    >
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2 border-b bg-muted/50">
        <div className="flex items-center gap-2">
          <div
            className={cn(
              isConnected
                ? 'status-dot-success'
                : isConnecting
                  ? 'status-dot-warning animate-pulse'
                  : 'status-dot-error'
            )}
          />
          <span className="font-medium text-sm">{container.name}</span>
          {sessionId && (
            <span className="text-xs text-muted-foreground">
              Session: {sessionId.slice(0, 8)}
            </span>
          )}
        </div>
        <div className="flex items-center gap-1">
          {error && (
            <Button
              variant="ghost"
              size="sm"
              onClick={handleReconnect}
              disabled={isConnecting}
              title="Reconnect"
            >
              {isConnecting ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <RefreshCw className="h-4 w-4" />
              )}
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={toggleMaximize}
            title={isMaximized ? 'Minimize' : 'Maximize'}
          >
            {isMaximized ? (
              <Minimize2 className="h-4 w-4" />
            ) : (
              <Maximize2 className="h-4 w-4" />
            )}
          </Button>
          <Button variant="ghost" size="sm" onClick={handleClose} title="Close">
            <X className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {/* Terminal container */}
      <div
        className="flex-1 p-1 bg-[#1e1e1e]"
        onClick={() => xtermRef.current?.focus()}
      >
        <div ref={terminalRef} className="h-full w-full" />
      </div>

      {/* Status bar */}
      <div className="flex items-center justify-between px-4 py-1 border-t bg-muted/50 text-xs text-muted-foreground">
        <span>
          {isConnecting
            ? 'Connecting...'
            : isConnected
              ? 'Connected'
              : error
                ? `Error: ${error}`
                : 'Disconnected'}
        </span>
        <span>
          {xtermRef.current ? `${xtermRef.current.cols}x${xtermRef.current.rows}` : ''}
        </span>
      </div>
    </div>
  );
}
