import federation from "@originjs/vite-plugin-federation";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const apiGateway = process.env.KACHO_API_BASE || "http://localhost:8080";
const kratos = process.env.KACHO_KRATOS_BASE || "http://localhost:4433";
const kratosUi = process.env.KACHO_KRATOS_UI_BASE || "http://localhost:4300";
const hydra = process.env.KACHO_HYDRA_BASE || "http://localhost:4444";
const kratosUiRoutes = [
  "/login",
  "/registration",
  "/recovery",
  "/verification",
  "/settings",
  "/error",
  "/consent",
  "/logout",
];

export default defineConfig({
  base: process.env.KACHO_PUBLIC_BASE || "/",
  plugins: [
    react(),
    federation({
      name: "dashboard",
      filename: "remoteEntry.js",
      exposes: {
        "./DashboardPage": "./src/pages/DashboardPage/index.ts",
        "./navigation": "./src/navigation.ts",
      },
      shared: ["antd", "lucide-react", "react", "react-dom"],
    }),
  ],
  server: {
    proxy: {
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
      "/iam/v1": {
        target: apiGateway,
        changeOrigin: true,
      },
      "/operations": {
        target: apiGateway,
        changeOrigin: true,
      },
      "/healthz": {
        target: apiGateway,
        changeOrigin: true,
      },
      "/readyz": {
        target: apiGateway,
        changeOrigin: true,
      },
      "/.ory/kratos/public": {
        target: kratos,
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/\.ory\/kratos\/public/, ""),
      },
      "/self-service": {
        target: kratos,
        changeOrigin: true,
      },
      "/.ory/hydra/public": {
        target: hydra,
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/\.ory\/hydra\/public/, ""),
      },
      "/oauth2": {
        target: hydra,
        changeOrigin: true,
      },
      ...Object.fromEntries(
        kratosUiRoutes.map((route) => [
          route,
          {
            target: kratosUi,
            changeOrigin: true,
          },
        ]),
      ),
    },
  },
  build: {
    target: "esnext",
    modulePreload: false,
    cssCodeSplit: false,
  },
});
