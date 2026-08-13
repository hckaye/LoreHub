"use client";

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";

type SidebarMenuProps = {
  children: (close: () => void) => ReactNode;
  menuClassName: string;
  summary: string;
};

export function SidebarMenu(props: SidebarMenuProps) {
  const [open, setOpen] = useState(false);
  const detailsRef = useRef<HTMLDetailsElement>(null);
  const summaryRef = useRef<HTMLElement>(null);
  const close = useCallback(() => setOpen(false), []);
  useEffect(() => {
    if (!open) return;
    function onPointerDown(event: PointerEvent) {
      if (!detailsRef.current?.contains(event.target as Node)) setOpen(false);
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== "Escape") return;
      setOpen(false);
      summaryRef.current?.focus();
    }
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);
  return (
    <details onToggle={(event) => setOpen(event.currentTarget.open)} open={open} ref={detailsRef}>
      <summary ref={summaryRef}>{props.summary}</summary>
      <div className={props.menuClassName}>{props.children(close)}</div>
    </details>
  );
}
