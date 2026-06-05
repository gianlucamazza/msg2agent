import type { ComponentChildren } from "preact";
interface Props { children: ComponentChildren; class?: string; }
export function Table({ children, class: cls }: Props) {
  return <div class={["table-wrapper", cls].filter(Boolean).join(" ")}><table>{children}</table></div>;
}
export function Thead({ children }: { children: ComponentChildren }) {
  return <thead>{children}</thead>;
}
export function Tbody({ children }: { children: ComponentChildren }) {
  return <tbody>{children}</tbody>;
}
export function Th({ children }: { children: ComponentChildren }) {
  return <th>{children}</th>;
}
export function Td({ children, class: cls }: { children?: ComponentChildren; class?: string }) {
  return <td class={cls}>{children}</td>;
}
