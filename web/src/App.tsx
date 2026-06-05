import { BrowserRouter, Routes, Route } from "react-router-dom";
import { Toaster } from "sonner";
import Layout from "./components/Layout";
import ErrorBoundary from "./components/ErrorBoundary";
import Dashboard from "./pages/Dashboard";
import Projects from "./pages/Projects";
import Files from "./pages/Files";
import FileTimeline from "./pages/FileTimeline";
import DiffPage from "./pages/DiffPage";
import SessionAnalysis from "./pages/SessionAnalysis";
import TrendsAnalysis from "./pages/TrendsAnalysis";
import NotFound from "./pages/NotFound";

function App() {
  return (
    <BrowserRouter>
      <Layout>
        <ErrorBoundary>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/projects" element={<Projects />} />
            <Route path="/projects/:project/files" element={<Files />} />
            <Route path="/projects/:project/files/*" element={<FileTimeline />} />
            <Route path="/projects/:project/files/*/diff" element={<DiffPage />} />
            <Route path="/projects/:project/session/:sessionId" element={<SessionAnalysis />} />
            <Route path="/projects/:project/trends" element={<TrendsAnalysis />} />
            <Route path="*" element={<NotFound />} />
          </Routes>
        </ErrorBoundary>
      </Layout>
      <Toaster position="top-right" theme="dark" />
    </BrowserRouter>
  );
}

export default App;
