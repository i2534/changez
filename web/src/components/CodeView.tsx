import Prism from "prismjs";
import "prismjs/themes/prism-tomorrow.min.css";
import "prismjs/components/prism-typescript.min.js";
import "prismjs/components/prism-go.min.js";
import "prismjs/components/prism-markup.min.js";
import "prismjs/components/prism-bash.min.js";
import { useMemo } from "react";

const LANG_MAP: Record<string, string> = {
  ".ts": "typescript",
  ".tsx": "typescript",
  ".js": "javascript",
  ".jsx": "javascript",
  ".go": "go",
  ".md": "markup",
  ".sh": "bash",
  ".bash": "bash",
  ".yaml": "yaml",
  ".yml": "yaml",
  ".json": "json",
  ".toml": "ini",
  ".ini": "ini",
  ".css": "css",
  ".html": "markup",
  ".xml": "markup",
};

function detectLanguage(filePath: string): string | null {
  const ext = filePath.slice(filePath.lastIndexOf("."));
  return LANG_MAP[ext] || null;
}

function highlightLine(text: string, lang: string): string {
  const grammar = Prism.languages[lang as keyof typeof Prism.languages];
  if (!grammar) return escapeHtml(text);
  const tokenized = Prism.tokenize(text, grammar);
  return Prism.Token.stringify(tokenized, lang);
}

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

export default function CodeView({
  content,
  filePath,
}: {
  content: string;
  filePath: string;
}) {
  const lang = detectLanguage(filePath);

  const highlightedLines = useMemo(() => {
    const lines = content.split("\n");
    if (!lang) return lines.map(escapeHtml);
    return lines.map((line) => highlightLine(line, lang));
  }, [content, lang]);

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
        <table className="w-full">
          <tbody>
            {highlightedLines.map((line, i) => (
              <tr key={i} className="hover:bg-gray-750">
                <td className="select-none border-r border-gray-700 bg-gray-800 px-3 py-0.5 text-right text-xs text-gray-500">
                  {i + 1}
                </td>
                <td
                  className="whitespace-pre font-mono text-sm text-gray-200 px-3 py-0.5"
                  dangerouslySetInnerHTML={{ __html: line || "\u00A0" }}
                />
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
