import { createContext, ReactNode, useContext } from 'react';
import { createPortal } from 'react-dom';

interface TopBarSlots {
  metaEl: HTMLElement | null;
  actionsEl: HTMLElement | null;
}

export const TopBarContext = createContext<TopBarSlots>({ metaEl: null, actionsEl: null });

/** Renders children into the sticky topbar's meta slot ("updated 12s ago"). */
export function TopBarMeta({ children }: { children: ReactNode }) {
  const { metaEl } = useContext(TopBarContext);
  if (!metaEl) return null;
  return createPortal(children, metaEl);
}

/** Renders children into the sticky topbar's right-aligned actions slot. */
export function TopBarActions({ children }: { children: ReactNode }) {
  const { actionsEl } = useContext(TopBarContext);
  if (!actionsEl) return null;
  return createPortal(children, actionsEl);
}
