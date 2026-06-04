import Prism from "prismjs";
import "prismjs/themes/prism-tomorrow.min.css";
import "prismjs/components/prism-typescript.min.js";
import "prismjs/components/prism-go.min.js";
import "prismjs/components/prism-markup.min.js";
import "prismjs/components/prism-bash.min.js";
import { useCallback, useRef } from "react";
import type { RowComponentProps } from "react-window";
import { List } from "react-window";
import { detectLanguage } from "../utils";

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

const ROW_HEIGHT = 22;
const AUTO_HEIGHT = 400;
const MAX_HEIGHT = 800;

const CodeViewRowComponent = ({ index, style, highlightLine }: RowComponentProps<{ highlightLine: (index: number) => string }>) => (
  <div style={style} className="flex hover:bg-gray-700">
    <div className="select-none border-r border-gray-700 bg-gray-800 px-3 text-right text-xs text-gray-500" style={{ height: ROW_HEIGHT, lineHeight: `${ROW_HEIGHT}px` }}>
      {index + 1}
    </div>
    <div
      className="flex-1 whitespace-pre font-mono text-sm text-gray-200 px-3"
      style={{ height: ROW_HEIGHT, lineHeight: `${ROW_HEIGHT}px` }}
      dangerouslySetInnerHTML={{ __html: highlightLine(index) || "\u00A0" }}
    />
  </div>
);

export default function CodeView({
  content,
  filePath,
  height,
}: {
  content: string;
  filePath: string;
  height?: number;
}) {
  const lang = detectLanguage(filePath);
  const lines = content.split("\n");
  const grammar = lang ? Prism.languages[lang as keyof typeof Prism.languages] : null;

  const highlightCache = useRef(new Map<number, string>());

  const highlightLine = useCallback(
    (index: number): string => {
      const cached = highlightCache.current.get(index);
      if (cached !== undefined) return cached;
      const text = lines[index];
      let result: string;
      if (grammar) {
        const tokenized = Prism.tokenize(text, grammar);
        result = Prism.Token.stringify(tokenized, lang!);
      } else {
        result = escapeHtml(text);
      }
      highlightCache.current.set(index, result);
      return result;
    },
    [grammar, lang, lines],
  );

   const listHeight = height ?? Math.min(Math.max(lines.length * ROW_HEIGHT, AUTO_HEIGHT), MAX_HEIGHT);

  return (
    <div>
      <div className="mb-2 flex items-center justify-between text-sm text-gray-400">
        <span>{filePath}</span>
        {lang && (
          <span className="rounded bg-gray-700 px-1.5 py-0.5 text-xs text-gray-500">
            {lang}
          </span>
        )}
      </div>
      <div className="overflow-x-auto rounded-lg bg-gray-800">
        {content === "" ? (
          <div className="flex h-[400px] items-center justify-center text-sm text-gray-500">
            Empty file
          </div>
        ) : (
          <List
            rowCount={lines.length}
            rowHeight={ROW_HEIGHT}
            rowComponent={CodeViewRowComponent}
            rowProps={{ highlightLine }}
            style={{ height: listHeight, width: "100%" }}
          />
        )}
      </div>
    </div>
  );
}
