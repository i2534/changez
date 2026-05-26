import { useEffect, useRef, useCallback } from "react";
import "diff2html/bundles/css/diff2html.min.css";

let d2hModule: typeof import("diff2html/bundles/js/diff2html-ui.min.js") | null = null;

async function loadD2H() {
  if (!d2hModule) {
    d2hModule = await import("diff2html/bundles/js/diff2html-ui.min.js");
  }
  return d2hModule;
}

export default function DiffViewer({
  diff,
  fromVersion,
  toVersion,
}: {
  diff: string;
  fromVersion: number;
  toVersion: number;
}) {
  const containerRef = useRef<HTMLDivElement>(null);

  const draw = useCallback(() => {
    loadD2H().then(({ Diff2HtmlUI }) => {
      if (!containerRef.current) return;
      containerRef.current.innerHTML = "";
      const d2h = new Diff2HtmlUI(
        containerRef.current,
        diff,
        {
          drawFileList: false,
          matching: "lines",
          outputFormat: "side-by-side",
          highlight: true,
          fileContentToggle: false,
        }
      );
      d2h.draw();
    });
  }, [diff]);

  useEffect(() => {
    draw();
  }, [draw]);

  return (
    <div>
      <div className="mb-3 flex items-center gap-3 text-sm text-gray-400">
        <span>
          v<strong>{fromVersion}</strong> → v<strong>{toVersion}</strong>
        </span>
      </div>
      <div className="d2h-dark-color-scheme overflow-x-auto rounded-lg bg-gray-800 p-2">
        <div ref={containerRef} />
      </div>
    </div>
  );
}
