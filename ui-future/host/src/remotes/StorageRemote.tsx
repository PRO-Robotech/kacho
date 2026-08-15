import { makeRemote, type RemotePageProps } from "./makeRemote";
import { moduleLabelOf } from "./moduleCatalog";
import type { ComponentType } from "react";

export const StorageRemote = makeRemote(
  () => import("storage/StoragePage"),
  (mod) => (mod.default ?? mod.StoragePage) as ComponentType<RemotePageProps> | undefined,
  moduleLabelOf("storage"),
);
