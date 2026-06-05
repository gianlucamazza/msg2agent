import type { ComponentProps } from "preact";
interface Props extends ComponentProps<"div"> {}
export default function Card({ class: cls, ...rest }: Props) {
  return <div class={["card", cls].filter(Boolean).join(" ")} {...rest} />;
}
