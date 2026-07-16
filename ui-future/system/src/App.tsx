import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import SystemPage from "@/pages/SystemPage";

// Standalone dev entry. Mirrors how the host mounts the remote — SystemPage owns
// everything under /system/* (regions/zones/address-pools/cluster-admins +
// /system/tokens/*), so its internal absolute links (AdminLayout / TokensLayout
// tabs, navigation.ts) resolve identically in standalone and federated runs.
export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/system/*" element={<SystemPage />} />
        <Route path="*" element={<Navigate to="/system/regions" replace />} />
      </Routes>
    </BrowserRouter>
  );
}
