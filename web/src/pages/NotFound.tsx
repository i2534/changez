import { useNavigate } from "react-router-dom";
export default function NotFound() {
  const navigate = useNavigate();
  return (
    <div className="flex min-h-[400px] items-center justify-center">
      <div className="text-center">
        <h2 className="text-lg font-bold text-gray-200">404 - Page Not Found</h2>
        <p className="mt-2 text-sm text-gray-400">The page you're looking for doesn't exist.</p>
        <button onClick={() => navigate("/")} className="mt-4 rounded bg-blue-600 px-3 py-1.5 text-sm text-white hover:bg-blue-500">
          Go Home
        </button>
      </div>
    </div>
  );
}
