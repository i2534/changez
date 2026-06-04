const variants = {
  card: "h-24 rounded-lg",
  row: "h-10 rounded",
  timeline: "h-16 rounded-lg",
  block: "h-64 rounded-lg",
} as const;

export interface SkeletonProps {
  variant?: keyof typeof variants;
  className?: string;
}

export default function Skeleton({ variant = "row", className }: SkeletonProps) {
  return (
    <div
      className={`animate-pulse bg-gray-800 ${variants[variant]}${className ? " " + className : ""}`}
    />
  );
}
