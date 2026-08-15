import { makeRemote, type RemotePageProps } from "./makeRemote";
import { moduleLabelOf } from "./moduleCatalog";
import type { ComponentType } from "react";

export const ComputeRemote = makeRemote(
  () => import("compute/InstancesPage"),
  (mod) => (mod.default ?? mod.InstancesPage) as ComponentType<RemotePageProps> | undefined,
  moduleLabelOf("compute"),
);
