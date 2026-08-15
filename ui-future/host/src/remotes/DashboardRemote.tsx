import { makeRemote, type RemotePageProps } from "./makeRemote";
import { moduleLabelOf } from "./moduleCatalog";
import type { ComponentType } from "react";

export const DashboardRemote = makeRemote(
  () => import("dashboard/DashboardPage"),
  (mod) => (mod.default ?? mod.DashboardPage) as ComponentType<RemotePageProps> | undefined,
  moduleLabelOf("dashboard"),
  "Загрузка dashboard",
);
