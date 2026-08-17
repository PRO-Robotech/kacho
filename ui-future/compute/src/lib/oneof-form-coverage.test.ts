// Ветви создания машины — против формы МОДУЛЯ `compute` (#375).
//
// Маршрут `/projects/:projectId/compute/*` обслуживает этот модуль и его
// реестр, поэтому спрашивать надо ЕГО: те же ветви, заведённые в `@shared`, до
// пользователя не доезжают — реестр `shared` обслуживает vpc/iam/system.

import { REGISTRY } from "./resource-registry";
import { oneofBranches } from "@shared/test/oneof-branch-coverage";
import { instanceBody, localDiskRefusal } from "@shared/test/instance-branch-coverage";

describe("вид машины: каждая ветвь спецификации выразима формой", () => {
  const spec = REGISTRY["compute-instances"];

  it("перечень контракта совпадает с перечнем, который даёт форма", () => {
    const contract = oneofBranches("compute/v1/instance_service.proto", "CreateInstanceRequest", "spec");
    const форма: Record<string, Record<string, unknown>> = {
      vm_spec: instanceBody(spec, "VM"),
      container_spec: instanceBody(spec, "CONTAINER"),
    };
    const выразимо = contract.filter((branch) => (форма[branch] ?? {})[branch] !== undefined);
    expect(выразимо).toEqual(contract);
  });

  it("вторая ветвь в теле не появляется — отрицание в паре с положительным", () => {
    expect(instanceBody(spec, "VM")).not.toHaveProperty("container_spec");
    expect(instanceBody(spec, "CONTAINER")).not.toHaveProperty("vm_spec");
  });
});

describe("ветвь местного диска формы не имеет — и у этого есть предикат снятия", () => {
  it("родительское поле ветви отвергается сервером синхронно, с именем поля", () => {
    // Ветвь `AttachedLocalDiskSpec.type` недостижима не потому, что о ней
    // забыли: её родитель `local_disk_specs` отвергается на транспортном слое,
    // называя поле. Это второй из трёх законных исходов, и форма для отвергаемого
    // поля была бы мёртвым интерфейсом.
    //
    // Отказ читается ИЗ ДЕРЕВА: исчезнет он — исчезнет и основание не иметь
    // формы, и эта проба покраснеет сама, не дожидаясь, пока кто-нибудь вспомнит.
    const refusal = localDiskRefusal();
    // Отказ не найден означает одно из двух: поле стало приниматься — и тогда
    // ветви `physical_local_disk` нужна форма; либо снято с контракта — и тогда
    // вместе с ним снимается эта проба.
    expect(refusal).toBeTruthy();
    expect(refusal).toContain("not supported");
  });

  it("ветвь существует в контракте — иначе у этой пробы нет предмета", () => {
    // Самоистечение с другой стороны: снимут ветвь с контракта — проба скажет,
    // что стеречь больше нечего.
    expect(oneofBranches("compute/v1/instance_service.proto", "AttachedLocalDiskSpec", "type")).toEqual([
      "physical_local_disk",
    ]);
  });
});
