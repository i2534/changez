import { SVGProps } from "react";

interface IconProps extends Omit<SVGProps<SVGSVGElement>, "size"> {
  size?: number;
}

export function FileIcon({ size = 16, className, ...props }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      {...props}
    >
      <path d="M6 1h4l4 4v9a1 1 0 01-1 1H3a1 1 0 01-1-1V2a1 1 0 011-1h4z" />
      <path d="M10 1v4h4" />
    </svg>
  );
}

export function FolderIcon({ size = 16, className, ...props }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      {...props}
    >
      <path d="M2 4V3a1 1 0 011-1h3l1.2 1.2H13a1 1 0 011 1v8a1 1 0 01-1 1H3a1 1 0 01-1-1V4z" />
    </svg>
  );
}

export function FolderOpenIcon({ size = 16, className, ...props }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      {...props}
    >
      <path d="M2 4V3a1 1 0 011-1h3l1.2 1.2H13a1 1 0 011 1v.5" />
      <path d="M1 7l2.5 7h9L15 7H1z" />
    </svg>
  );
}

export function TrashIcon({ size = 16, className, ...props }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      {...props}
    >
      <path d="M3 4h10" />
      <path d="M5 4V3a1 1 0 011-1h4a1 1 0 011 1v1" />
      <path d="M4 4l1 9a1 1 0 001 1h4a1 1 0 001-1l1-9" />
      <path d="M7 7v4" />
      <path d="M9 7v4" />
    </svg>
  );
}

export function ActionDot({ size = 10, className, ...props }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 10 10"
      fill="currentColor"
      className={className}
      {...props}
    >
      <circle cx="5" cy="5" r="4" />
    </svg>
  );
}

export function ActionSquare({ size = 10, className, ...props }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 10 10"
      fill="currentColor"
      className={className}
      {...props}
    >
      <rect x="1" y="1" width="8" height="8" rx="1.5" />
    </svg>
  );
}

export function ChevronRight({ size = 16, className, ...props }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      {...props}
    >
      <path d="M6 4l4 4-4 4" />
    </svg>
  );
}

export function ChevronDown({ size = 16, className, ...props }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      {...props}
    >
      <path d="M4 6l4 4 4-4" />
    </svg>
  );
}

export function CloseIcon({ size = 16, className, ...props }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      {...props}
    >
      <path d="M4 4l8 8" />
      <path d="M12 4l-8 8" />
    </svg>
  );
}

export function ActionIcon({ action, className = "" }: { action: string; className?: string }) {
  return (
    <span className={`inline-block align-text-bottom ${className}`}>
      {action === "delete" ? <ActionSquare size={10} /> : <ActionDot size={10} />}
    </span>
  );
}
