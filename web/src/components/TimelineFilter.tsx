import { useTranslation } from "react-i18next";

const ALL_ACTIONS = ["create", "update", "delete"];
export { ALL_ACTIONS };

interface TimelineFilterProps {
  sources: string[];
  sourceFilter: string;
  actionFilter: string;
  hasFilters: boolean;
  filteredCount: number;
  totalCount: number;
  versionJumpRef: React.RefObject<HTMLInputElement>;
  versionJump: string;
  onVersionJumpChange: (value: string) => void;
  onVersionJumpSubmit: () => void;
  onSourceChange: (value: string) => void;
  onActionChange: (value: string) => void;
  onClear: () => void;
}

export function TimelineFilter({
  sources,
  sourceFilter,
  actionFilter,
  hasFilters,
  filteredCount,
  totalCount,
  versionJumpRef,
  versionJump,
  onVersionJumpChange,
  onVersionJumpSubmit,
  onSourceChange,
  onActionChange,
  onClear,
}: TimelineFilterProps) {
  const { t } = useTranslation();

  return (
    <div className="mb-3 flex flex-wrap items-center gap-3">
      <input
        ref={versionJumpRef}
        type="number"
        value={versionJump}
        onChange={(e) => onVersionJumpChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            onVersionJumpSubmit();
          }
        }}
        placeholder={t("timeline.version_jump_placeholder")}
        className="w-28 rounded border border-gray-600 bg-gray-700 px-2 py-1 text-sm text-gray-200 placeholder-gray-500 focus:border-blue-500 focus:outline-none"
      />
      <select
        value={sourceFilter}
        onChange={(e) => onSourceChange(e.target.value)}
        className="rounded border border-gray-600 bg-gray-700 px-2 py-1 text-sm text-gray-200 focus:border-blue-500 focus:outline-none"
      >
        <option value="">{t("timeline.all_sources")}</option>
        {sources.map((s) => (
          <option key={s} value={s}>
            {s}
          </option>
        ))}
      </select>
      <select
        value={actionFilter}
        onChange={(e) => onActionChange(e.target.value)}
        className="rounded border border-gray-600 bg-gray-700 px-2 py-1 text-sm text-gray-200 focus:border-blue-500 focus:outline-none"
      >
        <option value="">{t("timeline.all_actions")}</option>
        {ALL_ACTIONS.map((a) => (
          <option key={a} value={a}>
            {a}
          </option>
        ))}
      </select>
      {hasFilters && (
        <button
          onClick={onClear}
          className="rounded border border-gray-600 bg-gray-700 px-2 py-1 text-sm text-gray-400 hover:text-gray-200"
        >
          {t("common.clear")}
        </button>
      )}
      {hasFilters && (
        <span className="self-center text-xs text-gray-500">
          {filteredCount} / {totalCount}
        </span>
      )}
    </div>
  );
}
