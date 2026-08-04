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
      name: "iam",
      filename: "remoteEntry.js",
      exposes: {
        "./IamPage": "./src/pages/IamPage/index.ts",
        "./navigation": "./src/navigation.ts",
      },
      shared: ["antd", "lucide-react", "react", "react-dom", "react-router"],
    }),
  ],
  resolve: {
    alias: {
      "@shared": path.resolve(__dirname, "../shared/src"),
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/iam/v1": {
        target: apiGateway,
        changeOrigin: true,
      },
      // Редактор правил роли рендерит picker реальных инстансов и для НЕ-iam
      // типов (перечень токенов — shared/src/lib/resourceInstanceFetchers.ts:
      // vpc-, compute- и nlb-ресурсы), поэтому их List обязан резолвиться и в
      // standalone-dev. Без прокси запрос гасится catch'ем в RulesEditor и
      // picker молча деградирует до свободного ввода id — отказ, который ничего
      // не печатает.
      "/vpc": {
        target: apiGateway,
        changeOrigin: true,
      },
      "/compute": {
        target: apiGateway,
        changeOrigin: true,
      },
      "/nlb": {
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
