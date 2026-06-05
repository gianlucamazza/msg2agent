interface Props { title: string; subtitle?: string; }
export default function EmptyState({ title, subtitle }: Props) {
  return (
    <div class="empty-state">
      <p class="empty-state-title">{title}</p>
      {subtitle && <p class="empty-state-subtitle">{subtitle}</p>}
    </div>
  );
}
