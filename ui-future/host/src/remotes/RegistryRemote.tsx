import { makeRemote, type RemotePageProps } from "./makeRemote";
import { moduleLabelOf } from "./moduleCatalog";
import type { ComponentType } from "react";

// Прежде здесь стояла своя копия связки lazy()+Suspense — форк makeRemote (см.
// NlbRemote). Сведено к общей фабрике: граница отказа заводится один раз и
// достаётся каждому модулю.
export const RegistryRemote = makeRemote(
  () => import("registry/RegistryPage"),
  (mod) => (mod.default ?? mod.RegistryPage) as ComponentType<RemotePageProps> | undefined,
  moduleLabelOf("registry"),
);
