import { makeRemote, type RemotePageProps } from "./makeRemote";
import type { ComponentType } from "react";

export const SystemRemote = makeRemote(
  () => import("system/SystemPage"),
  (mod) => (mod.default ?? mod.SystemPage) as ComponentType<RemotePageProps> | undefined,
);
