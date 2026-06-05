interface Props { class?: string; }
export default function Skeleton({ class: cls }: Props) {
  return <div class={["skeleton", cls].filter(Boolean).join(" ")} />;
}
