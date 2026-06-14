import { cn } from "@/lib/utils";
import { t, type Lang } from "@/lib/i18n";

export interface Role {
  uuid: string;
  name: string;
}

/** Single-select role pills (All + each role). Horizontally scrollable. */
export function RoleFilter({
  roles,
  selected,
  onSelect,
  lang,
}: {
  roles: Role[];
  selected: string | null; // null = All
  onSelect: (uuid: string | null) => void;
  lang: Lang;
}) {
  return (
    <div className="no-scrollbar flex gap-2 overflow-x-auto px-4 py-2.5">
      <Pill active={selected === null} onClick={() => onSelect(null)}>
        {t(lang, "roleAll")}
      </Pill>
      {roles.map((r) => (
        <Pill key={r.uuid} active={selected === r.uuid} onClick={() => onSelect(r.uuid)}>
          {r.name}
        </Pill>
      ))}
    </div>
  );
}

function Pill({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "shrink-0 rounded-full border px-3.5 py-1.5 text-sm font-semibold transition-colors duration-150",
        active
          ? "border-transparent bg-accent text-on-accent"
          : "border-hairline bg-surface text-fg-dim md:hover:border-fg-mute/70 md:hover:text-fg active:bg-surface-hi",
      )}
    >
      {children}
    </button>
  );
}
