// Чем вкладка дочернего ресурса просит сузить список — и когда не просит ничем.
//
// Пара обязательна в обе стороны: утверждение «параметр отправлен» зеленело бы и
// на функции, которая отправляет что-нибудь всегда, а утверждение «ничего не
// отправлено» — на функции, которая не отправляет ничего никогда. Поэтому здесь
// стоят оба механизма и оба отрицания.

import { relatedListQuery } from "./related-list-query";

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
