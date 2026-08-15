import { makeRemote, type RemotePageProps } from "./makeRemote";
import { moduleLabelOf } from "./moduleCatalog";
import type { ComponentType } from "react";

// Прежде здесь стояла своя копия связки lazy()+Suspense — форк makeRemote. Она
// разошлась с оригиналом ровно там, где это дороже всего: границы отказа у неё
// не было бы даже после правки makeRemote. Один предмет — один вид (ui.md, п.3).
export const NlbRemote = makeRemote(
  () => import("nlb/NlbPage"),
  (mod) => (mod.default ?? mod.NlbPage) as ComponentType<RemotePageProps> | undefined,
  moduleLabelOf("nlb"),
);
