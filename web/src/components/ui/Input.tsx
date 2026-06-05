import type { ComponentProps } from "preact";
interface Props extends ComponentProps<"input"> {}
export default function Input({ class: cls, ...rest }: Props) {
  return <input class={["form-input", cls].filter(Boolean).join(" ")} {...rest} />;
}
