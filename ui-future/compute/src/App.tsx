import type { FC } from "react";
import { BrowserRouter } from "react-router-dom";
import { InstancesPage } from "@/pages";

// Standalone-обёртка compute-remote (полная интеграция). В федеративном режиме
// host монтирует InstancesPage напрямую; здесь она поднимается со своим роутером
// для локального запуска/preview.
const App: FC = () => (
  <BrowserRouter>
    <InstancesPage />
  </BrowserRouter>
);

export default App;
