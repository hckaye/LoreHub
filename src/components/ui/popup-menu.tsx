"use client";

import { usePathname } from "next/navigation";
import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type ReactNode,
  type RefObject,
} from "react";
import { createPortal } from "react-dom";

import styles from "./popup-menu.module.css";

/**
 * Every open popup registers its close function here so that opening one popup
 * closes the others, even when the new popup is opened from the keyboard.
 */
const openPopups = new Set<() => void>();

type DismissOptions = {
  close: () => void;
  /** Interaction inside one of these elements keeps the popup open. */
  containers: RefObject<HTMLElement | null>[];
  focusOnDismiss?: RefObject<HTMLElement | null>;
  open: boolean;
};

/**
 * Closes a popup when the pointer goes down outside of it, when Escape is
 * pressed, and when another popup opens.
 */
export function useDismissOnOutsideInteraction({ close, containers, focusOnDismiss, open }: DismissOptions) {
  const containersRef = useRef(containers);
  useEffect(() => {
    containersRef.current = containers;
  });

  useEffect(() => {
    if (!open) {
      return;
    }
    for (const closeOther of openPopups) {
      closeOther();
    }
    openPopups.add(close);
    function handlePointerDown(event: PointerEvent) {
      const target = event.target as Node;
      if (containersRef.current.some((container) => container.current?.contains(target))) {
        return;
      }
      close();
    }
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key !== "Escape") {
        return;
      }
      event.preventDefault();
      close();
      focusOnDismiss?.current?.focus();
    }
    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      openPopups.delete(close);
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [close, focusOnDismiss, open]);
}

type TriggerProps = Omit<
  ButtonHTMLAttributes<HTMLButtonElement>,
  "aria-controls" | "aria-expanded" | "aria-haspopup" | "children" | "className" | "id" | "onClick" | "type"
>;

type PanelPosition = {
  left?: number;
  right?: number;
  top: number;
};

/** Pixels between the trigger and its panel. */
const panelOffset = 6;

/** Above every other layer in the application. */
const panelLayer = 60;

type PopupMenuProps = {
  /** Which edge the panel is anchored to; controls the open animation origin. */
  align?: "start" | "end";
  children: (close: () => void) => ReactNode;
  className?: string;
  panelClassName?: string;
  panelLabel?: string;
  /** Pass "none" when the panel holds form controls instead of menu items. */
  panelRole?: "menu" | "dialog" | "listbox" | "none";
  trigger: ReactNode;
  triggerClassName?: string;
  triggerProps?: TriggerProps;
};

/**
 * Tracks where the panel has to sit. Panels render in the document body so that
 * a scrolling or clipping ancestor of the trigger cannot cut them off, which
 * means they follow their trigger through scrolling and resizing instead.
 */
function usePanelPosition({
  align,
  enabled,
  triggerRef,
}: {
  align: "start" | "end";
  enabled: boolean;
  triggerRef: RefObject<HTMLButtonElement | null>;
}): PanelPosition | null {
  const [position, setPosition] = useState<PanelPosition | null>(null);

  useEffect(() => {
    if (!enabled) {
      return;
    }
    function follow() {
      const bounds = triggerRef.current?.getBoundingClientRect();
      if (!bounds) {
        return;
      }
      const top = bounds.bottom + panelOffset;
      setPosition(align === "end" ? { right: window.innerWidth - bounds.right, top } : { left: bounds.left, top });
    }
    follow();
    window.addEventListener("resize", follow);
    window.addEventListener("scroll", follow, true);
    return () => {
      window.removeEventListener("resize", follow);
      window.removeEventListener("scroll", follow, true);
    };
  }, [align, enabled, triggerRef]);

  return enabled ? position : null;
}

function PopupPanel({
  align,
  children,
  className,
  id,
  label,
  labelledBy,
  panelRef,
  position,
  role,
}: {
  align: "start" | "end";
  children: ReactNode;
  className?: string;
  id: string;
  label?: string;
  labelledBy: string;
  panelRef: RefObject<HTMLDivElement | null>;
  position: PanelPosition;
  role: "menu" | "dialog" | "listbox" | "none";
}) {
  return (
    <div
      aria-label={label}
      aria-labelledby={label ? undefined : labelledBy}
      className={className ? `${styles.panel} ${className}` : styles.panel}
      data-align={align}
      id={id}
      ref={panelRef}
      role={role === "none" ? undefined : role}
      style={{ position: "fixed", zIndex: panelLayer, ...position }}
    >
      {children}
    </div>
  );
}

export function PopupMenu({
  align = "end",
  children,
  className,
  panelClassName,
  panelLabel,
  panelRole = "menu",
  trigger,
  triggerClassName,
  triggerProps,
}: PopupMenuProps) {
  const pathname = usePathname() ?? "";
  const containerRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const generatedId = useId();
  const panelId = `popup-menu-${generatedId}`;
  const triggerId = `${panelId}-trigger`;
  // Storing the route the popup was opened on closes it after a navigation.
  const [openRoute, setOpenRoute] = useState<string | null>(null);
  const open = openRoute === pathname;
  const close = useCallback(() => setOpenRoute(null), []);
  const position = usePanelPosition({ align, enabled: open, triggerRef });

  useDismissOnOutsideInteraction({
    close,
    containers: [containerRef, panelRef],
    focusOnDismiss: triggerRef,
    open,
  });

  const panel = open && position && (
    <PopupPanel
      align={align}
      className={panelClassName}
      id={panelId}
      label={panelLabel}
      labelledBy={triggerId}
      panelRef={panelRef}
      position={position}
      role={panelRole}
    >
      {children(close)}
    </PopupPanel>
  );

  return (
    <div className={className} ref={containerRef}>
      <button
        {...triggerProps}
        aria-controls={open ? panelId : undefined}
        aria-expanded={open}
        aria-haspopup={panelRole === "none" ? true : panelRole}
        className={triggerClassName}
        id={triggerId}
        onClick={() => setOpenRoute(open ? null : pathname)}
        ref={triggerRef}
        type="button"
      >
        {trigger}
      </button>
      {panel && createPortal(panel, document.body)}
    </div>
  );
}
