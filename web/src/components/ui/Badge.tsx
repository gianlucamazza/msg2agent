type Variant = "success" | "danger" | "warn" | "default";
interface Props { label: string; variant?: Variant; }
export default function Badge({ label, variant = "default" }: Props) {
  return <span class={`badge badge-${variant}`}>{label}</span>;
}
