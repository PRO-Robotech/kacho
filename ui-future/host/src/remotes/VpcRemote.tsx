import { makeRemote, type RemotePageProps } from "./makeRemote";
import { moduleLabelOf } from "./moduleCatalog";
import type { ComponentType } from "react";

export const VpcRemote = makeRemote(
  () => import("vpc/VpcPage"),
  (mod) => (mod.default ?? mod.VpcPage) as ComponentType<RemotePageProps> | undefined,
  moduleLabelOf("vpc"),
);
