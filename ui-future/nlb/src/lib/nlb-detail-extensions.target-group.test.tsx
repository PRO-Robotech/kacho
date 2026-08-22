// Блок «Обзор» целевой группы — сверка с контрактом ствола.
//
// Ground truth: proto/kacho/cloud/loadbalancer/v1/target_group.proto и
// health_check.proto.
//
//  * TargetGroup несёт `deregistration_delay` типа google.protobuf.Duration
//    (protojson рендерит её строкой «300s»); прежнее целочисленное имя
//    `deregistration_delay_seconds` зарезервировано (target_group.proto:28-29).
//    Реестр форм это уже знает — resource-registry.target-group.test.ts, — а
//    страница ресурса читала снятое имя, то есть ДВА места об одном предмете,
//    из которых верно одно.
//  * HealthCheck больше не именованный ресурс: поле `name` зарезервировано
//    (health_check.proto:27-28). Показывать нечего и эквивалента у него нет —
//    вместо имени содержательна выбранная ветвь пробы (tcp|http|https|grpc) и
//    производное `effective_port`.

import { render } from "@testing-library/react";
import type { ReactNode } from "react";

// Реестр расширений — ОБЩИЙ (`@shared/…`), а содержимое кладёт в него раздел
// побочным действием импорта ниже — ровно так, как это делает точка входа
// (`NlbPage`). Прежде проба брала и то и другое у местного бареля
// `@/components/organisms/ResourceDetailExtensions`; тот импортировал
// регистрацию сам, поэтому утверждение оставалось истинным и тогда, когда
// продукт её не подключил. Барель снят вместе со сведением форка оболочки —
// в прод-коде его не звал никто, — и проба переехала к своему предмету,
// в `lib/`, где живут сами расширения.
import "@/lib/nlb-detail-extensions";

import { detailExtension, type DescItem } from "@shared/components/organisms/ResourceDetailExtensions";

function itemsFor(data: Record<string, unknown>): DescItem[] {
  const ext = detailExtension("target-groups");
  if (!ext?.overviewExtra) throw new Error("у target-groups нет overviewExtra");
  return ext.overviewExtra({ data, projectId: "prj-1", detailBase: "/x", navigate: () => {} });
}

function textOf(value: ReactNode): string {
  return render(<div>{value}</div>).container.textContent ?? "";
}

// Подпись строки — ReactNode (в неё кладут ⓘ-подсказку), поэтому сверять её
// надо ПОКАЗАННЫМ текстом, а не приведением типа: `String(<узел>)` дал бы
// «[object Object]» для любой подписи, и проба стала бы истинной при любой.
function labelText(i: DescItem): string {
  return textOf(i.label);
}

function labels(items: DescItem[]): string[] {
  return items.map(labelText);
}

const TG = {
  id: "tg-000000000000000",
  region_id: "ru-central1",
  port: 8080,
  deregistration_delay: "300s",
  slow_start: "30s",
  status: "ACTIVE",
  health_check: { http: { port: 8081, path: "/healthz" }, effective_port: 8081, interval: "2s" },
  // Снятые имена в ответе ствола не приезжают; кладём их здесь намеренно, чтобы
  // проба отличала «прочитано верное поле» от «совпало по случайности».
  deregistration_delay_seconds: 42,
} as Record<string, unknown>;

describe("обзор целевой группы против контракта ствола", () => {
  it("drain-таймаут читается из Duration-поля, а не из снятого целочисленного", () => {
    const items = itemsFor(TG);
    const drain = items.find((i) => /вывода из-под нагрузки/i.test(labelText(i)));
    expect(drain).toBeDefined();
    expect(textOf(drain!.value)).toContain("300s");
    expect(textOf(drain!.value)).not.toContain("42");
  });

  it("подпись drain-строки не обещает секунды, которых в ответе нет", () => {
    const drain = itemsFor(TG).find((i) => /вывода из-под нагрузки/i.test(labelText(i)))!;
    expect(labelText(drain)).not.toMatch(/\(с\)/);
  });

  it("проба показывается выбранной ветвью и портом, а не снятым именем", () => {
    const items = itemsFor(TG);
    const hc = items.find((i) => /проверка состояния/i.test(labelText(i)));
    expect(hc).toBeDefined();
    const text = textOf(hc!.value);
    expect(text).toContain("http");
    expect(text).toContain("8081");
  });

  it("группа без пробы не притворяется настроенной", () => {
    // Положительный контроль отрицания выше: без health_check строка обязана
    // остаться пустой, а не показать выдуманную ветвь.
    const items = itemsFor({ ...TG, health_check: undefined });
    const hc = items.find((i) => /проверка состояния/i.test(labelText(i)))!;
    expect(textOf(hc.value)).toBe("—");
  });

  it("порт группы виден — он единственный источник backend-порта", () => {
    expect(labels(itemsFor(TG)).join(" ")).toMatch(/порт/i);
  });
});
