import { makeRemote, type RemotePageProps } from "./makeRemote";
import { moduleLabelOf } from "./moduleCatalog";
import type { ComponentType } from "react";

export const IamRemote = makeRemote(
  () => import("iam/IamPage"),
  (mod) => (mod.default ?? mod.IamPage) as ComponentType<RemotePageProps> | undefined,
  moduleLabelOf("iam"),
);
