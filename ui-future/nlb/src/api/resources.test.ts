import { targetGroupWiring } from "./resources";

// Целевые группы балансировщика выводятся из ЕГО ЛИСТЕНЕРОВ.
//
// Привязка живёт на `Listener.target_group_id` (легаси-зеркало —
// `default_target_group_id`). Прежний M:N-снимок `attached_target_groups` на
// самом балансировщике снят с контракта вместе с глаголами
// `:attachTargetGroup` / `:detachTargetGroup`: обоих маршрутов край не
// обслуживает вовсе, поэтому список привязок было НЕ ИЗ ЧЕГО выводить, а кнопки
// «Привязать»/«Отвязать» уходили в 404.
describe("target groups of a load balancer are derived from its listeners", () => {
  it("берёт target_group_id листенера", () => {
    expect(targetGroupWiring([{ id: "lst-1", name: "http", target_group_id: "tgr-1" }])).toEqual([
      { targetGroupId: "tgr-1", listeners: [{ id: "lst-1", name: "http" }] },
    ]);
  });

  it("принимает легаси-зеркало default_target_group_id", () => {
    expect(targetGroupWiring([{ id: "lst-1", name: "http", default_target_group_id: "tgr-1" }])).toEqual([
      { targetGroupId: "tgr-1", listeners: [{ id: "lst-1", name: "http" }] },
    ]);
  });

  it("одна группа на нескольких листенерах — одна строка, все листенеры названы", () => {
    expect(
      targetGroupWiring([
        { id: "lst-1", name: "http", target_group_id: "tgr-1" },
        { id: "lst-2", name: "https", target_group_id: "tgr-1" },
      ]),
    ).toEqual([
      {
        targetGroupId: "tgr-1",
        listeners: [
          { id: "lst-1", name: "http" },
          { id: "lst-2", name: "https" },
        ],
      },
    ]);
  });

  it("листенер без группы (substatus MISCONFIGURED) не порождает строки", () => {
    expect(targetGroupWiring([{ id: "lst-1", name: "http", target_group_id: "" }])).toEqual([]);
    expect(targetGroupWiring([{ id: "lst-1", name: "http" }])).toEqual([]);
    expect(targetGroupWiring(undefined)).toEqual([]);
  });

  it("листенер без имени назван своим идентификатором", () => {
    expect(targetGroupWiring([{ id: "lst-1", target_group_id: "tgr-1" }])).toEqual([
      { targetGroupId: "tgr-1", listeners: [{ id: "lst-1", name: "lst-1" }] },
    ]);
  });
});
