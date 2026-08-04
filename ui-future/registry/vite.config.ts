import path from "node:path";
import federation from "@originjs/vite-plugin-federation";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const apiGateway = process.env.KACHO_API_BASE || "http://localhost:8080";
const kratos = process.env.KACHO_KRATOS_BASE || "http://localhost:4433";
const hydra = process.env.KACHO_HYDRA_BASE || "http://localhost:4444";

export default defineConfig({
  base: process.env.KACHO_PUBLIC_BASE || "/",
  plugins: [
    react(),
    federation({
      name: "registry",
      filename: "remoteEntry.js",
      exposes: {
        "./RegistryPage": "./src/pages/RegistryPage/index.ts",
        "./navigation": "./src/navigation.ts",
      },
      shared: ["antd", "lucide-react", "react", "react-dom", "react-router"],
    }),
  ],
  resolve: {
    // Одна копия каждой библиотеки с внутренним состоянием на бандл.
    //
    // Этот remote ставит зависимости САМ (свой package-lock, свой node_modules),
    // а исходники @shared лежат вне его дерева, поэтому у резолвера появляется
    // выбор, откуда взять react: снизу (свой) или сверху (workspace-установка
    // рядом). Две копии react означают null-диспетчер хуков, две копии
    // react-router / react-query — контексты, которых провайдер приложения не
    // видит; сборка при этом проходит, ломается рантайм.
    //
    // Замерено: сейчас vite и так резолвит их в node_modules этого пакета —
    // dedupe закрепляет это правилом, а не совпадением. В jest выбор ушёл в
    // другую сторону и хуки общего компонента падали (см. moduleNameMapper).
    dedupe: ["react", "react-dom", "react-router", "@tanstack/react-query", "antd"],
    alias: {
      "@shared": path.resolve(__dirname, "../shared/src"),
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/registry": {
        target: apiGateway,
        changeOrigin: true,
      },
      // Справочник размещения — домен geo. Реестр ресурсов этого приложения
      // объявляет /geo/v1/regions и /geo/v1/zones; без записи прокси запрос до
      // края не доходит вовсе, и список выглядит пустым, а не отказавшим.
      "/geo": {
        target: apiGateway,
        changeOrigin: true,
      },
      "/iam/v1": {
        target: apiGateway,
        changeOrigin: true,
      },
      "/operations": {
        target: apiGateway,
        changeOrigin: true,
      },
      "/.ory/kratos/public": {
        target: kratos,
        changeOrigin: true,
        rewrite: (urlPath) => urlPath.replace(/^\/\.ory\/kratos\/public/, ""),
      },
      "/.ory/hydra/public": {
        target: hydra,
        changeOrigin: true,
        rewrite: (urlPath) => urlPath.replace(/^\/\.ory\/hydra\/public/, ""),
      },
    },
  },
  build: {
    target: "esnext",
    modulePreload: false,
    cssCodeSplit: false,
  },
});
