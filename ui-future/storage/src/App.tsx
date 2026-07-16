import type { FC } from "react";
import { BrowserRouter } from "react-router-dom";
import { StoragePage } from "@/pages";

// Standalone-обёртка storage-remote (полная интеграция). В федеративном режиме
// host монтирует StoragePage напрямую; здесь она поднимается со своим роутером
// для локального запуска/preview.
const App: FC = () => (
  <BrowserRouter>
    <StoragePage />
  </BrowserRouter>
);

export default App;
