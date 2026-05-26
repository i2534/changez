export default function StatCard({
  label,
  value,
  onClick,
}: {
  label: string;
  value: number;
  onClick?: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className={`rounded-lg bg-gray-800 p-5 text-left transition-colors ${
        onClick ? "cursor-pointer hover:bg-gray-700 hover:shadow-lg" : "cursor-default"
      }`}
    >
      <div className="text-2xl font-bold text-gray-100">{value.toLocaleString()}</div>
      <div className="mt-1 text-sm text-gray-400">{label}</div>
    </button>
  );
}
