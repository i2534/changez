import { useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { relativeTime } from "../utils";
import { File } from "../api/types";

type TreeNode =
  | { type: "dir"; name: string; path: string; children: TreeNode[] }
  | { type: "file"; name: string; file: File };

function buildTree(files: File[]): TreeNode[] {
  const root: TreeNode[] = [];
  const dirMap = new Map<string, TreeNode>();

  const sorted = [...files].sort((a, b) => a.path.localeCompare(b.path));

  for (const file of sorted) {
    const segments = file.path.split("/");
    const fileName = segments[segments.length - 1];

    let currentPath = "";
    let currentDir = { children: root };

    for (const seg of segments.slice(0, -1)) {
      currentPath = currentPath ? `${currentPath}/${seg}` : seg;

      if (!dirMap.has(currentPath)) {
        const dirNode: TreeNode = { type: "dir", name: seg, path: currentPath, children: [] };
        dirMap.set(currentPath, dirNode);
        const { children } = currentDir;
        let inserted = false;
        for (let i = 0; i < children.length; i++) {
          if (children[i].type === "dir" && children[i].name > seg) {
            children.splice(i, 0, dirNode);
            inserted = true;
            break;
          }
        }
        if (!inserted) children.push(dirNode);
      }

      const dirNode = dirMap.get(currentPath)! as { type: "dir"; name: string; path: string; children: TreeNode[] };
      currentDir = dirNode;
    }

    const { children } = currentDir;
    const fileNode: TreeNode = { type: "file", name: fileName, file };
    let inserted = false;
    for (let i = 0; i < children.length; i++) {
      if (children[i].type === "file" && children[i].name > fileName) {
        children.splice(i, 0, fileNode);
        inserted = true;
        break;
      }
    }
    if (!inserted) children.push(fileNode);
  }

  return root;
}

function getMatchingAncestors(tree: TreeNode[], query: string): Set<string> {
  const ancestors = new Set<string>();

  function walk(nodes: TreeNode[], path: string) {
    for (const node of nodes) {
      if (node.type === "dir") {
        const childPath = path ? `${path}/${node.name}` : node.name;
        let hasMatch = false;
        walk(node.children, childPath);
        if (ancestors.size > 0 && !ancestors.has(childPath)) {
          function checkDescendant(n: TreeNode): boolean {
            if (n.type === "file") {
              return n.file.path.toLowerCase().includes(query);
            }
            return n.children.some(checkDescendant);
          }
          hasMatch = checkDescendant(node);
        }
        if (hasMatch) ancestors.add(childPath);
      } else {
        if (node.file.path.toLowerCase().includes(query)) {
          if (path) ancestors.add(path);
        }
      }
    }
  }

  walk(tree, "");
  return ancestors;
}

function TreeNodeView({
  node,
  depth,
  expanded,
  onToggle,
  onFileClick,
}: {
  node: TreeNode;
  depth: number;
  expanded: Set<string>;
  onToggle: (path: string) => void;
  onFileClick: (path: string) => void;
}) {
  const { t } = useTranslation();
  if (node.type === "file") {
    return (
      <button
        onClick={() => onFileClick(node.file.path)}
        className="flex w-full items-center justify-between rounded px-2 py-1.5 text-left hover:bg-gray-700"
        style={{ paddingLeft: `${depth * 16 + 8}px` }}
      >
        <span className="truncate text-sm text-gray-200">
          <span className="mr-2">📄</span>
          {node.name}
        </span>
        <div className="ml-4 flex shrink-0 items-center gap-3 text-xs text-gray-400">
          {node.file.latestVersionId != null && (
            <span>v{node.file.latestVersionId}</span>
          )}
          <span>{relativeTime(node.file.createdAt, t)}</span>
        </div>
      </button>
    );
  }

  const isExpanded = expanded.has(node.path) || false;
  return (
    <div>
      <button
        onClick={() => onToggle(node.path)}
        className="flex w-full items-center rounded px-2 py-1.5 text-left hover:bg-gray-700"
        style={{ paddingLeft: `${depth * 16 + 8}px` }}
      >
        <span
          className={`mr-1 inline-block transition-transform ${isExpanded ? "rotate-90" : ""}`}
        >
          ▶
        </span>
        <span className="mr-2">📁</span>
        <span className="text-sm text-gray-200">{node.name}</span>
      </button>
      {isExpanded && (
        <TreeNodeList
          nodes={node.children}
          depth={depth + 1}
          expanded={expanded}
          onToggle={onToggle}
          onFileClick={onFileClick}
        />
      )}
    </div>
  );
}

function TreeNodeList({
  nodes,
  depth,
  expanded,
  onToggle,
  onFileClick,
}: {
  nodes: TreeNode[];
  depth: number;
  expanded: Set<string>;
  onToggle: (path: string) => void;
  onFileClick: (path: string) => void;
}) {
  return (
    <>
      {nodes.map((node, i) => {
        const key = `${depth}-${node.type}-${node.name}-${i}`;
        return (
          <TreeNodeView
            key={key}
            node={node}
            depth={depth}
            expanded={expanded}
            onToggle={onToggle}
            onFileClick={onFileClick}
          />
        );
      })}
    </>
  );
}

export default function FileList({
  files,
  onFileClick,
  searchQuery,
  onSearchChange,
}: {
  files: File[];
  onFileClick: (path: string) => void;
  searchQuery: string;
  onSearchChange: (q: string) => void;
}) {
  const [expanded, _setExpanded] = useState<Set<string>>(new Set());
void _setExpanded;
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const { t } = useTranslation();

  const filtered = useMemo(() => {
    if (!searchQuery.trim()) return files;
    const q = searchQuery.toLowerCase();
    return files.filter((f) => f.path.toLowerCase().includes(q));
  }, [files, searchQuery]);

  const tree = useMemo(() => buildTree(filtered), [filtered]);

  const autoExpanded = useMemo(() => {
    if (!searchQuery.trim()) return new Set<string>();
    const q = searchQuery.toLowerCase();
    return getMatchingAncestors(tree, q);
  }, [tree, searchQuery]);

  const effectiveExpanded = useMemo(() => {
    const merged = new Set<string>(expanded);
    for (const p of autoExpanded) merged.add(p);
    for (const p of collapsed) merged.delete(p);
    return merged;
  }, [expanded, autoExpanded, searchQuery, collapsed]);

  const toggle = (path: string) => {
    const next = new Set(collapsed);
    if (effectiveExpanded.has(path)) {
      next.add(path);
    } else {
      next.delete(path);
    }
    setCollapsed(next);
  };

  return (
    <div>
      <div className="mb-4">
        <input
          type="text"
          value={searchQuery}
          onChange={(e) => onSearchChange(e.target.value)}
          placeholder={t("files.search_placeholder")}
          className="w-full rounded border border-gray-600 bg-gray-700 px-3 py-2 text-sm text-gray-100 placeholder-gray-400 focus:border-blue-500 focus:outline-none"
        />
      </div>

      <div className="space-y-0.5">
        <TreeNodeList
          nodes={tree}
          depth={0}
          expanded={effectiveExpanded}
          onToggle={toggle}
          onFileClick={onFileClick}
        />
      </div>

      {filtered.length === 0 && (
        <p className="py-8 text-center text-sm text-gray-500">{t("files.no_files")}</p>
      )}
    </div>
  );
}
