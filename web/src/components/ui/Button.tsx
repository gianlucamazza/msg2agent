import type { ComponentProps } from "preact";
type Variant = "primary" | "secondary" | "ghost" | "danger";
type Size = "sm" | "md";
interface Props extends ComponentProps<"button"> {
  variant?: Variant;
  size?: Size;
}
export default function Button({ variant = "secondary", size = "md", class: cls, ...rest }: Props) {
  const base = `btn-${variant}`;
  const sz = size === "sm" ? "btn-sm" : "";
  return <button class={[base, sz, cls].filter(Boolean).join(" ")} {...rest} />;
}
