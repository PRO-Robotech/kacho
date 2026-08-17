// Чем вкладка дочернего ресурса просит сузить список — и когда не просит ничем.
//
// Пара обязательна в обе стороны: утверждение «параметр отправлен» зеленело бы и
// на функции, которая отправляет что-нибудь всегда, а утверждение «ничего не
// отправлено» — на функции, которая не отправляет ничего никогда. Поэтому здесь
// стоят оба механизма и оба отрицания.

import { childListPathScope, relatedListQuery } from "./related-list-query";

describe("сужение вкладки просит сервер — ровно одним из двух механизмов", () => {
  it("типизированное поле уезжает отдельным параметром", () => {
    expect(relatedListQuery({ serverParamField: "subnet_id" }, "sub-1")).toEqual({ subnet_id: "sub-1" });
  });

  it("поле белого списка уезжает выражением фильтра", () => {
    expect(relatedListQuery({ serverFilterField: "network_id" }, "net-1")).toEqual({
      filter: 'network_id="net-1"',
    });
  });

  it("ничего не объявлено — параметр не выдумывается", () => {
    expect(relatedListQuery({}, "net-1")).toBeUndefined();
  });

  it("родитель ещё не прочитан — запрос не сужается наугад", () => {
    // Пустой идентификатор родителя означает «карточка ещё грузится». Сужение по
    // нему отдало бы пустой список и выдало бы его за ответ.
    expect(relatedListQuery({ serverParamField: "subnet_id" }, "")).toBeUndefined();
    expect(relatedListQuery({ serverFilterField: "network_id" }, "")).toBeUndefined();
  });
});

// Ребёнок, адресуемый ПУТЁМ, а не параметром запроса.
//
// `/registry/v1/registries/{registryId}/repositories/{repository}/tags` — путь с
// ДВУМЯ подстановками: сервер сужает список сам, по сегментам. Значения берутся
// с РОДИТЕЛЬСКОЙ строки, а не выдумываются: репозиторий несёт `registry_id`, а
// собственного `id` у него нет вовсе (натуральный ключ — пара «реестр + имя»),
// поэтому недостающую подстановку закрывает идентичность родителя — ровно то
// значение, которым он адресован в URL карточки.
describe("childListPathScope: ребёнок адресуется путём", () => {
  const TAGS = "/registry/v1/registries/{registryId}/repositories/{repository}/tags";
  const REPOS = "/registry/v1/registries/{registryId}/repositories";

  it("две подстановки закрываются полем родителя и его идентичностью", () => {
    const out = childListPathScope(TAGS, ["registry_id", "repository"], { registry_id: "reg-1", name: "backend/api" }, "backend/api");
    expect(out.pathScoped).toBe(true);
    expect(out.pathParams).toEqual({ registry_id: "reg-1", repository: "backend/api" });
  });

  it("одна подстановка закрывается идентичностью родителя", () => {
    // Реестр несёт свой `id`, поля `registry_id` у него нет.
    const out = childListPathScope(REPOS, ["registry_id"], { id: "reg-1", name: "Продакшн" }, "reg-1");
    expect(out.pathScoped).toBe(true);
    expect(out.pathParams).toEqual({ registry_id: "reg-1" });
  });

  it("путь без подстановок — не path-scoped (положительный контроль)", () => {
    // Дети vpc сужаются параметром запроса; ветка пути их касаться не вправе.
    const out = childListPathScope("/vpc/v1/subnets", ["network_id"], { id: "net-1" }, "net-1");
    expect(out.pathScoped).toBe(false);
    expect(out.pathParams).toEqual({});
  });

  it("родитель ещё не прочитан — путь не считается заполненным", () => {
    // Иначе запрос уйдёт по пути с пустым сегментом и ответ выдадут за список.
    expect(childListPathScope(TAGS, ["registry_id", "repository"], {}, "").pathScoped).toBe(false);
  });

  it("подстановка, которую нечем закрыть, оставляет путь незаполненным", () => {
    // `{repository}` закрывается идентичностью, а `{registryId}` — только полем
    // родителя. Нет поля и идентичность уже израсходована — заполнять нечем.
    const out = childListPathScope(TAGS, ["registry_id", "repository"], { name: "backend/api" }, "backend/api");
    expect(out.pathParams.registry_id).toBeUndefined();
    expect(out.pathScoped).toBe(false);
  });

  it("поле родителя сильнее идентичности", () => {
    // У репозитория есть и `registry_id`, и идентичность. Подставить
    // идентичность в `{registryId}` значило бы адресовать реестр именем образа.
    const out = childListPathScope(TAGS, ["registry_id", "repository"], { registry_id: "reg-1", name: "nginx" }, "nginx");
    expect(out.pathParams.registry_id).toBe("reg-1");
  });

  it("подстановки, не названной связью, функция не выдумывает", () => {
    // Связь объявляет владелец ребёнка. Догадка консоли о недостающем сегменте
    // — это адрес, которого никто не объявлял.
    const out = childListPathScope(TAGS, ["registry_id"], { registry_id: "reg-1" }, "reg-1");
    expect(out.pathParams).toEqual({ registry_id: "reg-1" });
    expect(out.pathScoped).toBe(false);
  });
});
