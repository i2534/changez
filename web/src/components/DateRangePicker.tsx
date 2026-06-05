import { useState, useRef, useEffect } from "react";
import { DayPicker, UI, SelectionState, DayFlag } from "react-day-picker";
import { zhCN } from "date-fns/locale/zh-CN";
import { enUS } from "date-fns/locale/en-US";

interface DateRangePickerProps {
  since: string;
  until: string;
  onSinceChange: (date: string) => void;
  locale: string;
}

export default function DateRangePicker({
  since,
  until,
  onSinceChange,
  locale,
}: DateRangePickerProps) {
  const [open, setOpen] = useState<"since" | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const lang = locale === "zh" ? zhCN : enUS;
  const isZh = locale === "zh";

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(null);
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  const formatDate = (d: Date) => d.toISOString().slice(0, 10);

  const selectedDate = since ? new Date(since + "T00:00:00") : undefined;

  return (
    <div ref={ref} className="relative inline-flex items-center gap-2">
      <label className="flex items-center gap-2 text-xs text-gray-400">
        <span>{isZh ? "从" : "From"}</span>
        <button
          type="button"
          onClick={() => setOpen(open === "since" ? null : "since")}
          className="w-28 rounded border border-gray-700 bg-gray-900 px-2 py-1 text-left text-xs text-gray-300 transition-colors hover:border-gray-600 focus:border-blue-500 focus:outline-none"
        >
          {since || (isZh ? "选择日期" : "Pick a date")}
        </button>
      </label>
      <span className="text-xs text-gray-500">{isZh ? "到" : "To"}</span>
      <span className="w-28 rounded border border-gray-700 bg-gray-800/50 px-2 py-1 text-left text-xs text-gray-500 cursor-default select-none">
        {until}
      </span>

      {open === "since" && (
        <div className="absolute top-full left-0 z-50 mt-1 rounded-lg border border-gray-700 bg-gray-800 p-3 shadow-lg">
          <DayPicker
            mode="single"
            selected={selectedDate}
            defaultMonth={selectedDate ?? new Date()}
            locale={lang}
            onSelect={(date) => {
              if (date) {
                onSinceChange(formatDate(date));
                setOpen(null);
              }
            }}
            fixedWeeks
            classNames={{
              [UI.Root]: "rounded-lg",
              [UI.Months]: "flex flex-col sm:flex-row gap-4",
              [UI.Month]: "w-full",
              [UI.MonthCaption]: "flex justify-center pt-1 relative items-center",
              [UI.CaptionLabel]: "text-sm font-medium text-gray-200",
              [UI.Nav]: "flex items-center",
              [UI.PreviousMonthButton]: "absolute left-2 text-gray-400 hover:text-gray-200",
              [UI.NextMonthButton]: "absolute right-2 text-gray-400 hover:text-gray-200",
              [UI.Chevron]: "size-4",
              [UI.Weekdays]: "flex",
              [UI.Weekday]: "text-xs font-medium text-gray-500 w-9",
              [UI.MonthGrid]: "mt-2 w-full border-collapse",
              [UI.Weeks]: "",
              [UI.Week]: "flex w-full mt-1",
              [UI.Day]: "relative p-0 text-center flex items-center justify-center rounded-md text-xs h-9 w-9",
              [UI.DayButton]: "size-full rounded-md text-gray-300 hover:bg-gray-700",
              [SelectionState.selected]: "!bg-blue-500 !text-white hover:!bg-blue-600",
              [DayFlag.today]: "font-semibold text-blue-400",
              [DayFlag.outside]: "text-gray-600",
              [DayFlag.disabled]: "text-gray-700",
            }}
          />
        </div>
      )}
    </div>
  );
}
