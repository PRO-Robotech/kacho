import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import federation from "@originjs/vite-plugin-federation";

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
  plugins: [
    react(),
    federation({
      name: "host",
      remotes: {
        dashboard: process.env.KACHO_DASHBOARD_REMOTE || "http://localhost:4175/assets/remoteEntry.js",
        vpc: process.env.KACHO_VPC_REMOTE || "http://localhost:4176/assets/remoteEntry.js",
        iam: process.env.KACHO_IAM_REMOTE || "http://localhost:4177/assets/remoteEntry.js",
        nlb: process.env.KACHO_NLB_REMOTE || "http://localhost:4178/assets/remoteEntry.js",
        registry: process.env.KACHO_REGISTRY_REMOTE || "http://localhost:4179/assets/remoteEntry.js",
        system: process.env.KACHO_SYSTEM_REMOTE || "http://localhost:4180/assets/remoteEntry.js",
        compute: process.env.KACHO_COMPUTE_REMOTE || "http://localhost:4181/assets/remoteEntry.js",
        storage: process.env.KACHO_STORAGE_REMOTE || "http://localhost:4182/assets/remoteEntry.js",
      },
      shared: ["antd", "lucide-react", "react", "react-dom", "react-router-dom"],
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
      "/storage": {
        target: apiGateway,
        changeOrigin: true,
      },
      // System remote (mounted at /system/*) reads regions/zones from geo.
      "/geo": {
        target: apiGateway,
        changeOrigin: true,
      },
      "/nlb": {
        target: apiGateway,
        changeOrigin: true,
      },
      "/registry": {
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
  },
});
