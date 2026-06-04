import { useState, useCallback } from "react";
import { apiJSON } from "../api/client";
import { RestoreResponse } from "../api/types";
import { toast } from "sonner";

export interface FileContent {
  version: number;
  content: string;
}

export interface UseFileContentOptions {
  filePath: string;
  corruptedKey: string;
  failedKey: string;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export interface UseFileContentReturn {
  content: FileContent | null;
  isLoading: boolean;
  fetchContent: (versionId: number) => Promise<void>;
  clearContent: () => void;
}

export function useFileContent({
  filePath,
  corruptedKey,
  failedKey,
  t,
}: UseFileContentOptions): UseFileContentReturn {
  const [content, setContent] = useState<FileContent | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  const fetchContent = useCallback(
    async (versionId: number) => {
      try {
        setIsLoading(true);
        const data = await apiJSON<RestoreResponse>(
          `/api/files/restore?path=${encodeURIComponent(filePath)}&version=${versionId}`
        );
        setContent({ version: data.version, content: data.content });
      } catch (err) {
        const msg = err instanceof Error ? err.message : t(failedKey);
        if (msg.includes("CORRUPTED_DATA") || msg.includes("base_id")) {
          toast.error(t(corruptedKey, { version: versionId }));
        } else {
          toast.error(msg);
        }
      } finally {
        setIsLoading(false);
      }
    },
    [filePath, corruptedKey, failedKey, t]
  );

  const clearContent = useCallback(() => {
    setContent(null);
  }, []);

  return { content, isLoading, fetchContent, clearContent };
}
