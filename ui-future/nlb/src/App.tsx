import type { FC } from "react";
import { BrowserRouter } from "react-router-dom";
import { NlbPage } from "@/pages";

// Standalone-обёртка NLB-remote (полная интеграция). В федеративном режиме
// host монтирует NlbPage напрямую; здесь она поднимается со своим роутером
// для локального запуска/preview.
const App: FC = () => (
  <BrowserRouter>
    <NlbPage />
  </BrowserRouter>
);

export default App;
