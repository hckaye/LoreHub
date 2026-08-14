"use client";

import type { ReactNode } from "react";

import { PopupMenu } from "@/components/ui/popup-menu";

type SidebarMenuProps = {
  children: (close: () => void) => ReactNode;
  menuClassName: string;
  summary: string;
  triggerClassName?: string;
};

export function SidebarMenu(props: SidebarMenuProps) {
  return (
    <PopupMenu
      panelClassName={props.menuClassName}
      panelRole="none"
      trigger={props.summary}
      triggerClassName={props.triggerClassName}
    >
      {props.children}
    </PopupMenu>
  );
}
