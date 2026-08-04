// Блок «Обзор» балансировщика и листенера — против полей, которые домен УЖЕ
// объявил принимаемыми с провода.
//
// Ground truth: proto/kacho/cloud/loadbalancer/v1/{network_load_balancer,
// listener}.proto:
//   * NetworkLoadBalancer.admin_state = 41 (желаемое административное
//     состояние — замена снятым :start/:stop), cross_zone_enabled = 42
//     (REGIONAL-only), security_group_ids = 43 (INTERNAL-only, набор заменяется
//     целиком);
//   * Listener.target_group_id = 19 (целевая группа листенера),
//     resolved_backend_port = 20 (output-only эхо TargetGroup.port; 0 = ни одна
//     группа не резолвится), substatus = 21 (OK | MISCONFIGURED).
//
// Тип, объявленный в `api/types.ts`, — это обещание показать. Поле, которое
// приезжает и нигде не читается, — принято-и-проигнорировано на стороне
// клиента: человек не видит ни того, что балансировщик административно
// выключен, ни того, что листенер объявлен и обслуживать его некому.

import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import type { ReactNode } from "react";

import { detailExtension, type DescItem } from "./ResourceDetailExtensions";

function itemsFor(specId: string, data: Record<string, unknown>): DescItem[] {
  const ext = detailExtension(specId);
  if (!ext?.overviewExtra) throw new Error(`у ${specId} нет overviewExtra`);
  return ext.overviewExtra({ data, projectId: "prj-1", detailBase: "/x", navigate: () => {} });
}

// Ссылка на чужой ресурс (RefNameLink) резолвит имя через useQuery и строит
// SPA-маршрут, поэтому ей нужны клиент запросов и роутер. Бэкенда здесь нет
// (retry: false — без повторов и зависания): до загрузки ссылка показывает сам
// идентификатор, что и утверждается ниже.
function textOf(value: ReactNode): string {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>{value}</MemoryRouter>
      </QueryClientProvider>,
    ).container.textContent ?? ""
  );
}

function find(items: DescItem[], re: RegExp): DescItem {
  const hit = items.find((i) => re.test(i.label));
  if (!hit) throw new Error(`строки ${re} нет среди: ${items.map((i) => i.label).join(" | ")}`);
  return hit;
}

/** INTERNAL + REGIONAL — посадка, при которой применимы ОБА условных поля. */
const LB = {
  id: "nlb-000000000000000",
  region_id: "ru-central1",
  placement: "INTERNAL_REGIONAL",
  type: "INTERNAL",
  placement_type: "REGIONAL",
  admin_state: "ADMIN_STATE_DISABLED",
  cross_zone_enabled: true,
  security_group_ids: ["sg-000000000000001", "sg-000000000000002"],
  session_affinity: "FIVE_TUPLE",
  status: "ACTIVE",
  deletion_protection: false,
} as Record<string, unknown>;

const LST = {
  id: "lst-000000000000000",
  load_balancer_id: "nlb-000000000000000",
  protocol: "TCP",
  port: 443,
  target_port: 8443,
  target_group_id: "tg-000000000000009",
  resolved_backend_port: 8080,
  substatus: "MISCONFIGURED",
  status: "ACTIVE",
} as Record<string, unknown>;

describe("обзор балансировщика показывает объявленные поля", () => {
  it("административное состояние видно — иначе выключенный LB неотличим от рабочего", () => {
    const text = textOf(find(itemsFor("load-balancers", LB), /администрат|включ/i).value);
    expect(text).not.toBe("—");
    expect(text).not.toBe("");
  });

  it("выключённый и включённый различимы", () => {
    // Положительный близнец: строка, показывающая одно и то же при обоих
    // значениях, ничего не сообщает.
    const off = textOf(find(itemsFor("load-balancers", LB), /администрат|включ/i).value);
    const on = textOf(
      find(itemsFor("load-balancers", { ...LB, admin_state: "ADMIN_STATE_ENABLED" }), /администрат|включ/i).value,
    );
    expect(on).not.toBe(off);
  });

  it("балансировка между зонами видна на REGIONAL", () => {
    const text = textOf(find(itemsFor("load-balancers", LB), /зонами/i).value);
    expect(text).not.toBe("—");
    expect(text).not.toContain("true");
  });

  it("на ZONAL строки балансировки между зонами нет — поле неприменимо", () => {
    // Контроль в другую сторону: у зонального LB зона одна, и `true` там
    // отвергается сервером. Показывать его значение значило бы предлагать
    // настройку, которой у этой посадки нет.
    const items = itemsFor("load-balancers", {
      ...LB,
      placement: "INTERNAL_ZONAL",
      placement_type: "ZONAL",
      cross_zone_enabled: false,
    });
    expect(items.find((i) => /зонами/i.test(i.label))).toBeUndefined();
  });

  it("группы безопасности VIP перечислены поимённо", () => {
    const text = textOf(find(itemsFor("load-balancers", LB), /групп/i).value);
    expect(text).toContain("sg-000000000000001");
    expect(text).toContain("sg-000000000000002");
  });

  it("у EXTERNAL строки групп безопасности нет — они сетевые", () => {
    const items = itemsFor("load-balancers", {
      ...LB,
      placement: "EXTERNAL_REGIONAL",
      type: "EXTERNAL",
      placement_type: "",
      security_group_ids: [],
    });
    expect(items.find((i) => /групп/i.test(i.label))).toBeUndefined();
  });
});

describe("обзор листенера показывает объявленные поля", () => {
  it("целевая группа листенера видна", () => {
    const text = textOf(find(itemsFor("listeners", LST), /групп/i).value);
    expect(text).not.toBe("—");
  });

  it("порт бэкенда, переотражённый из группы, виден", () => {
    const text = textOf(find(itemsFor("listeners", LST), /бэкенд/i).value);
    expect(text).toContain("8080");
    // И это НЕ frontend-порт: путать их нельзя.
    expect(text).not.toContain("443");
  });

  it("нулевой переотражённый порт — прочерк, а не порт 0", () => {
    // 0 в контракте означает «ни одна группа не резолвится», а не номер порта.
    const text = textOf(find(itemsFor("listeners", { ...LST, resolved_backend_port: 0 }), /бэкенд/i).value);
    expect(text).toBe("—");
  });

  it("несконфигурированный листенер называет это прямо", () => {
    const text = textOf(find(itemsFor("listeners", LST), /обслуж|подстатус|состояни/i).value);
    expect(text).not.toBe("—");
    const ok = textOf(find(itemsFor("listeners", { ...LST, substatus: "OK" }), /обслуж|подстатус|состояни/i).value);
    expect(ok).not.toBe(text);
  });
});
