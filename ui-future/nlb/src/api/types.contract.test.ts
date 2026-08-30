// TS-типы домена балансировщика против контракта ствола.
//
// Ground truth: proto/kacho/cloud/loadbalancer/v1/{network_load_balancer,
// listener,target_group,health_check}.proto.
//
// Проверка двухслойная и слои разные по природе:
//
//  1) ТИПОВОЙ слой — присваивания ниже компилируются ts-jest'ом, поэтому
//     объектный литерал с полем, которого в интерфейсе нет, роняет СУИТУ
//     (excess property check), а отсутствующее обязательное поле — тоже.
//     Так «интерфейс отстал от контракта» становится наблюдаемым событием,
//     а не немой правдой в файле, который никто не читает.
//  2) ОТРИЦАТЕЛЬНЫЙ типовой слой — снятое в стволе имя интерфейс обязан
//     ОТВЕРГАТЬ. Утверждается тем же компилятором (`@ts-expect-error`): если
//     имя вернут в интерфейс, подавление станет лишним и ts-jest уронит суиту.
//     Прежняя редакция вместо этого читала СВОЙ ЖЕ `types.ts` с диска и искала
//     в тексте подстроки — то есть утверждала о символах файла, а не о том,
//     что тип принимает и что отвергает: такая проба переживала любую смену
//     формы записи, не покраснев.

import type { Listener, NetworkLoadBalancer, NlbHealthCheck, TargetGroup } from "./types";

describe("типы домена балансировщика против контракта ствола", () => {
  it("NetworkLoadBalancer несёт placement, admin_state и разделяемые SG", () => {
    const lb: NetworkLoadBalancer = {
      id: "nlb-000000000000000",
      project_id: "prj-1",
      region_id: "ru-central1",
      // placement — ЕДИНСТВЕННЫЙ авторитетный режим на Create (required);
      // type/placement_type — производные проекции только на чтение.
      placement: "INTERNAL_ZONAL",
      type: "INTERNAL",
      placement_type: "ZONAL",
      admin_state: "ADMIN_STATE_ENABLED",
      cross_zone_enabled: false,
      security_group_ids: ["sg-000000000000000"],
      disabled_announce_zones: [],
      session_affinity: "FIVE_TUPLE",
      deletion_protection: false,
      v4_address_id: "",
      v6_address_id: "",
      status: "ACTIVE",
    };
    expect(lb.placement).toBe("INTERNAL_ZONAL");
    expect(lb.admin_state).toBe("ADMIN_STATE_ENABLED");
  });

  it("Listener несёт целевую группу, разрешённый backend-порт и substatus", () => {
    const l: Listener = {
      id: "lsn-000000000000000",
      project_id: "prj-1",
      load_balancer_id: "nlb-000000000000000",
      protocol: "TCP",
      port: 443,
      // `target_port` здесь не задаётся: у сообщения `Listener` его номер и имя
      // зарезервированы (#512). Проба, задававшая его, ЗАКРЕПЛЯЛА поле, которого
      // у контракта нет, — то есть держала неверное утверждение зелёным.
      // Backend-порт читается из `resolved_backend_port` ниже.
      // Привязка целевой группы переехала со снятых глаголов балансировщика
      // на листенер.
      target_group_id: "tg-000000000000000",
      default_target_group_id: "",
      resolved_backend_port: 8080,
      substatus: "OK",
      status: "ACTIVE",
    };
    expect(l.target_group_id).not.toBe("");
    expect(l.substatus).toBe("OK");
  });

  it("TargetGroup несёт Duration-каналы, порт и состояние", () => {
    const tg: TargetGroup = {
      id: "tg-000000000000000",
      project_id: "prj-1",
      region_id: "ru-central1",
      port: 8080,
      // protojson рендерит Duration строкой секунд с хвостовым «s».
      deregistration_delay: "300s",
      slow_start: "0s",
      status: "ACTIVE",
    };
    expect(tg.deregistration_delay).toBe("300s");
    expect(tg.slow_start).toBe("0s");
  });

  it("HealthCheck — четырёхветвевая проба без имени", () => {
    // `name` снят (health_check.proto: reserved 1 / reserved "name"), поэтому
    // проба обязана быть выразима БЕЗ него.
    const hc: NlbHealthCheck = {
      http: { port: 8081, path: "/healthz", expected_codes: "200-299" },
      effective_port: 8081,
      interval: "2s",
      timeout: "1s",
      unhealthy_threshold: 2,
      healthy_threshold: 2,
    };
    expect(hc.effective_port).toBe(8081);

    const tcp: NlbHealthCheck = { tcp: { port: 80 } };
    const https: NlbHealthCheck = { https: { port: 443, path: "/healthz" } };
    const grpc: NlbHealthCheck = { grpc: { service_name: "grpc.health.v1.Health" } };
    expect([tcp.tcp, https.https, grpc.grpc].filter(Boolean)).toHaveLength(3);
  });

  it("снятое в стволе имя интерфейс отвергает", () => {
    // Утверждение отрицательное и проверяется КОМПИЛЯТОРОМ: вернут поле в
    // интерфейс — подавление станет лишним, и ts-jest уронит суиту. Без пары
    // с положительными присваиваниями выше оно зеленело бы и на пустом типе.
    const withRetired: TargetGroup = {
      id: "tg-000000000000000",
      project_id: "prj-1",
      region_id: "ru-central1",
      status: "ACTIVE",
      // @ts-expect-error deregistration_delay_seconds снят: канал объявлен Duration-строкой
      deregistration_delay_seconds: 300,
    };
    expect(withRetired.id).toBe("tg-000000000000000");

    const lbWithRetired: NetworkLoadBalancer = {
      id: "nlb-000000000000000",
      project_id: "prj-1",
      region_id: "ru-central1",
      placement: "INTERNAL_ZONAL",
      status: "ACTIVE",
      // @ts-expect-error привязка целевых групп переехала на листенер
      attached_target_groups: [],
    };
    expect(lbWithRetired.placement).toBe("INTERNAL_ZONAL");
  });
});
