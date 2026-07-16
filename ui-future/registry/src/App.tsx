import type { FC } from "react";
import { BrowserRouter } from "react-router-dom";
import { RegistryPage } from "@/pages";

// Standalone-обёртка registry-remote (полная интеграция). В федеративном режиме
// host монтирует RegistryPage напрямую; здесь она поднимается со своим роутером
// для локального запуска/preview.
const App: FC = () => (
  <BrowserRouter>
    <RegistryPage />
  </BrowserRouter>
);

export default App;
