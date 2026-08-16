// vpc.v1.StaticRoute.next_hop is a oneof: `next_hop_address` XOR `gateway_id`.
// The panel saves by REPLACING the whole static_routes list, so whatever it loaded
// into its drafts is what gets written back for every row — including rows the
// operator never touched.
//
// It used to load a gateway route's id into the address draft
// (`next_hop_address: r.next_hop_address ?? r.gateway_id ?? ""`). Saving after
// editing an unrelated row therefore rewrote that route's arm: a gateway id was
// stored as if it were an IP. Both names are declared fields, so the edge accepts
// the body — strict parsing would not catch it either.

import { draftsFromRoutes, routeGaps, routesFromDrafts } from "./RoutesPanel";

describe("routes drafts round-trip", () => {
  it("keeps a gateway route on its own arm through load and save", () => {
    const fromServer = [
      { destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" },
      { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" },
    ];
    const drafts = draftsFromRoutes(fromServer);
    expect(drafts[0]).toMatchObject({ destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1", next_hop_address: "" });
    expect(routesFromDrafts(drafts)).toEqual([
      { destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" },
      { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" },
    ]);
  });

  it("emits exactly one arm per route, never both", () => {
    for (const route of routesFromDrafts(draftsFromRoutes([{ destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" }]))) {
      expect(Object.keys(route).sort()).toEqual(["destination_prefix", "gateway_id"]);
    }
  });

  // Здесь стояла проба «drops a row with a destination but no next hop, and one
  // with neither» — она ЗАКРЕПЛЯЛА молчаливое удаление. Сохранение заменяет весь
  // список, поэтому отброшенная перед отправкой строка не «не сохраняется», а
  // УДАЛЯЕТСЯ: оператор, стерев адрес существующего маршрута, чтобы набрать его
  // заново, и нажав «Сохранить», терял маршрут целиком и получал сообщение об
  // успехе.
  //
  // При этом край такую строку принимать и не собирался: он отвечает
  // `InvalidArgument` с указанием поля и текстом
  // `static_routes[i]: next_hop_address or gateway_id is required`
  // (services/vpc/internal/apps/kacho/api/routetable/helpers.go). То есть отбор
  // не «оберегал» вызов от отказа — он подменял точный отказ края потерей данных.
  it("неполную строку не выбрасывает, а называет — строкой и тем, чего в ней нет", () => {
    const drafts = [
      { destination_prefix: "10.0.0.0/8", next_hop_address: "  " },
      { destination_prefix: "  ", next_hop_address: "10.0.0.1" },
      { destination_prefix: " 10.1.0.0/16 ", next_hop_address: " 10.1.0.1 " },
      { destination_prefix: "  ", next_hop_address: "  " },
    ];

    expect(routeGaps(drafts)).toEqual([
      { row: 1, missing: ["следующий узел"] },
      { row: 2, missing: ["префикс назначения"] },
      { row: 4, missing: ["префикс назначения", "следующий узел"] },
    ]);
  });

  it("полный набор строк претензий не вызывает — положительный контроль", () => {
    expect(
      routeGaps([
        { destination_prefix: "10.1.0.0/16", next_hop_address: "10.1.0.1" },
        { destination_prefix: "0.0.0.0/0", next_hop_address: "", gateway_id: "gtw-1" },
      ]),
    ).toEqual([]);
  });

  it("строка со шлюзом узел ИМЕЕТ — пустое поле адреса претензией не является", () => {
    expect(routeGaps([{ destination_prefix: "0.0.0.0/0", next_hop_address: "", gateway_id: "gtw-1" }])).toEqual([]);
  });

  it("сохранение уносит все строки, включая неполные, — отбора перед отправкой нет", () => {
    expect(
      routesFromDrafts([
        { destination_prefix: " 10.1.0.0/16 ", next_hop_address: " 10.1.0.1 " },
        { destination_prefix: "10.0.0.0/8", next_hop_address: "" },
      ]),
    ).toEqual([{ destination_prefix: "10.1.0.0/16", next_hop_address: "10.1.0.1" }, { destination_prefix: "10.0.0.0/8" }]);
  });

  it("метки строки переживают круг «загрузили → сохранили»", () => {
    const fromServer = [
      { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1", labels: { env: "prod" } },
      { destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1", labels: { env: "dev" } },
    ];

    expect(routesFromDrafts(draftsFromRoutes(fromServer))).toEqual(fromServer);
  });

  it("правка одной строки не трогает метки соседней", () => {
    const drafts = draftsFromRoutes([
      { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1", labels: { env: "prod" } },
      { destination_prefix: "192.168.0.0/16", next_hop_address: "10.0.0.2", labels: { env: "dev" } },
    ]);
    drafts[0].next_hop_address = "10.0.0.9";

    expect(routesFromDrafts(drafts)[1]).toEqual({
      destination_prefix: "192.168.0.0/16",
      next_hop_address: "10.0.0.2",
      labels: { env: "dev" },
    });
  });

  it("маршрут без меток их и не отращивает — пустая карта на край не уезжает", () => {
    expect(routesFromDrafts(draftsFromRoutes([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]))).toEqual(
      [{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }],
    );
  });

  it("an address typed over a gateway row replaces the arm, deliberately", () => {
    const drafts = draftsFromRoutes([{ destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" }]);
    drafts[0].next_hop_address = "10.0.0.1";
    expect(routesFromDrafts(drafts)).toEqual([{ destination_prefix: "0.0.0.0/0", next_hop_address: "10.0.0.1" }]);
  });
});
