import { createContext, useContext, type ReactNode } from "react";

interface TimelineContextValue {
  expandedId: number | null;
  setExpandedId: (id: number | null) => void;
  contentVersionId: number | null;
  setContentVersionId: (id: number | null) => void;
  contentMap: Map<number, string>;
  loadingContent: number | null;
  selectedIds: number[];
  filePath: string;
  project: string;
  t: (key: string, params?: Record<string, unknown>) => string;
  summaries: Map<number, { summary: string; status: string; model: string }>;
}

export const TimelineContext = createContext<TimelineContextValue | null>(null);

export interface TimelineProviderProps {
  value: TimelineContextValue;
  children: ReactNode;
}

export function TimelineProvider({ value, children }: TimelineProviderProps) {
  return (
    <TimelineContext.Provider value={value}>
      {children}
    </TimelineContext.Provider>
  );
}

export function useTimelineContext() {
  const ctx = useContext(TimelineContext);
  if (!ctx) {
    throw new Error("useTimelineContext must be used within TimelineProvider");
  }
  return ctx;
}
