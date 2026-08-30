// Доменные расширения карточки NLB — то немногое, что у раздела действительно
// СВОЁ: строки «Обзора» балансировщика / листенера / целевой группы, панель
// целей группы и вкладка «Целевые группы» балансировщика.
//
// Оболочка карточки при этом ОБЩАЯ (`@shared/components/organisms/ResourceShell`)
// и о существовании этого модуля не знает: она спрашивает расширение по
// `spec.id` у общего реестра, а модуль кладёт туда своё при загрузке
// (`registerDetailExtension`). Прежде вместо этого стояла копия самой оболочки —
// на 426 строк разошедшаяся с общей, — и правка общей до раздела не доезжала.
//
// Регистрация — ПОБОЧНЫМ ДЕЙСТВИЕМ импорта, а не вызовом из компонента: реестр
// спрашивают на рендере карточки, то есть заведомо позже загрузки модуля.
// Импортируют этот файл двое, и оба нужны: точка входа раздела
// (`pages/NlbPage`) — ради живого продукта, барель
// `components/organisms/ResourceDetailExtensions` — ради тех, кто спрашивает
// расширения через него.

import { type ReactNode } from "react";
import { Typography } from "antd";

import type { DetailTab } from "@/components/organisms/DetailShell";
import { RefNameLink } from "@/components/molecules/RefNameLink";
import { TargetsManager, type Target } from "@/components/organisms/TargetsManager";
import { LbTargetGroupsTab } from "@/components/organisms/LbTargetGroupsTab";
import { StatusBadge, statusPillStyle } from "@/components/atoms/StatusBadge";
import { NlbVipCell } from "@shared/components/molecules/NlbVipCell";
import { getByPath } from "@/lib/resource-registry";
// Логическое свойство — ОДНИМ рендером на всю консоль. Здесь стоял свой
// `boolTag(v, "Да", "Нет")`: он отвечал на вопрос, которого пользователь не
// задавал (правило 6 `ui.md`, где «Защита от удаления: Да» приведена дословным
// примером нарушения). Собственный рендер держался тем, что общий словарь
// логического варианта не нёс, — предмет исчез вместе с `format:"bool"`.
import { BoolFact } from "@shared/components/atoms/BoolFact";
// Форма строки обзора и сам реестр расширений — общие. Своих объявлений
// `DescItem`/`DetailExtCtx`/`DetailExtension` у раздела больше нет: три копии
// одного типа расходились бы молча, а общий уже несёт всё, что нужно NLB
// (`overviewExtra`, `overviewBelow`, `extraTabs`).
import {
  registerDetailExtension,
  type DescItem,
  type DetailExtension,
} from "@shared/components/organisms/ResourceDetailExtensions";

const dash = <Typography.Text type="secondary">—</Typography.Text>;

function code(v: unknown): ReactNode {
  const s = v == null ? "" : String(v);
  return s ? (
    <Typography.Text code style={{ fontSize: 12 }}>
      {s}
    </Typography.Text>
  ) : (
    dash
  );
}

/**
 * Административное состояние балансировщика — словом, а не именем константы.
 *
 * Это замена снятым глаголам `:start`/`:stop`: выключенный балансировщик
 * сохраняет конфигурацию, а его таргеты сообщаются как INACTIVE. Не показать
 * его значит оставить выключенный балансировщик неотличимым от рабочего —
 * ACTIVE в строке «Статус» стоит у обоих.
 */
function adminStateTag(v: unknown): ReactNode {
  switch (v) {
    case "ADMIN_STATE_ENABLED":
      return <span style={statusPillStyle("ok")}>Включён</span>;
    case "ADMIN_STATE_DISABLED":
      // Не «ошибка», а положение, о котором стоит знать: выключенный
      // балансировщик исправен и трафика не принимает.
      return <span style={statusPillStyle("warn")}>Выключен</span>;
    default:
      // UNSPECIFIED/пусто — сервер состояния не назвал; выдавать это за
      // «включён» нельзя.
      return dash;
  }
}

/**
 * Подстатус листенера — производное значение: резолвится ли его целевая группа.
 *
 * MISCONFIGURED значит «объявлен, обслуживать некому»; из строки «Статус» это не
 * видно — она остаётся ACTIVE. UNSPECIFIED/пусто выдавать за OK нельзя.
 */
function substatusTag(v: unknown): ReactNode {
  switch (v) {
    case "OK":
      return <span style={statusPillStyle("ok")}>Обслуживается</span>;
    case "MISCONFIGURED":
      return <span style={statusPillStyle("warn")}>Целевая группа не резолвится</span>;
    default:
      return dash;
  }
}

/**
 * Набор ссылок на чужие ресурсы; пустой набор — прочерк, а не пустая строка.
 *
 * Здесь стояли чипы с машинным идентификатором внутри: ни имени, ни перехода,
 * при том что соседние строки той же карточки («Балансировщик», «Целевая
 * группа») ссылкой уже были — один предмет выглядел двумя. Вид чипа сохранён
 * (`asTag`): это по-прежнему набор, а не одиночное значение.
 */
function refTags(v: unknown, specId: string, projectId: string | null): ReactNode {
  const ids = Array.isArray(v) ? (v as unknown[]).map(String).filter(Boolean) : [];
  if (ids.length === 0) return dash;
  return (
    <span style={{ display: "inline-flex", flexWrap: "wrap", gap: 4 }}>
      {ids.map((id) => (
        <RefNameLink key={id} specId={specId} refId={id} projectId={projectId ?? undefined} asTag maxChars={28} />
      ))}
    </span>
  );
}

/** Ветви пробы целевой группы — ровно одна из четырёх задана (oneof options). */
const HEALTH_CHECK_KINDS = ["tcp", "http", "https", "grpc"] as const;

/**
 * Краткое описание пробы: «<ветвь> :<порт>».
 *
 * Проба не именована — `name` снят с контракта, — поэтому отличать одну от
 * другой приходится тем, что проба собственно делает. Порт берётся из
 * производного `effective_port` (переопределение ветви, иначе порт группы):
 * расхождение порта пробы и порта трафика видно by construction. Ни одной
 * заданной ветви — пусто: молчание ответа не выдаём за настроенную проверку.
 */
function healthCheckSummary(data: Record<string, unknown>): string {
  const kind = HEALTH_CHECK_KINDS.find((k) => getByPath<unknown>(data, `health_check.${k}`) != null);
  if (!kind) return "";
  const port = getByPath<number>(data, "health_check.effective_port");
  return port ? `${kind} :${port}` : kind;
}

// ─────────────────────────── реестр ───────────────────────────

export const NLB_DETAIL_EXTENSIONS: Record<string, DetailExtension> = {
  "load-balancers": {
    // Единая таблица «Обзор»: immutable схема/размещение + mutable-скаляры +
    // резолвнутый VIP пофамильно + drain-зоны. Размещение — только для INTERNAL,
    // зоны без анонса — только для REGIONAL (зеркалит форму создания).
    //
    // Условные строки показываются ровно там, где поле применимо, — границы
    // взяты у владельца контракта, а не угаданы: cross_zone_enabled применим при
    // любом НЕ-зональном размещении (включая EXTERNAL, у которого placement_type
    // пуст), security_group_ids — только у INTERNAL (группы сетевые). Показывать
    // значение там, где сервер его отвергает, значит предлагать настройку,
    // которой у этой посадки нет.
    overviewExtra: ({ data, projectId }) => {
      const type = getByPath<string>(data, "type") ?? "";
      const placement = getByPath<string>(data, "placement_type") ?? "";
      const drainZones = (getByPath<string[]>(data, "disabled_announce_zones") ?? []) as string[];
      const items: DescItem[] = [
        {
          // Регион — ресурс каталога geo со своей карточкой: ссылка, как и в
          // списке балансировщиков. Плоский моноширинный идентификатор не давал
          // ни имени, ни перехода.
          label: "Регион",
          value: (
            <RefNameLink specId="regions" refId={getByPath<string>(data, "region_id")} maxChars={42} copy={false} />
          ),
          copy: getByPath<string>(data, "region_id") ?? undefined,
        },
        // Схема и размещение — машинные значения контракта, и показываются они
        // ТЕМ ЖЕ видом, что в списке (`format: "code"`), а не цветным тегом
        // чужой палитры: цвет следует за смыслом, а «внутренний» и «публичный»
        // ничего не сообщают о здоровье ресурса.
        { label: "Схема", value: code(type) },
      ];
      if (type === "INTERNAL") {
        items.push({ label: "Размещение", value: code(placement) });
      }
      items.push(
        { label: "Административное состояние", value: adminStateTag(getByPath<string>(data, "admin_state")) },
        { label: "Привязка сессий", value: code(getByPath<string>(data, "session_affinity")) },
        { label: "IPv4-адрес", value: <NlbVipCell v4AddressId={getByPath<string>(data, "v4_address_id")} /> },
        { label: "IPv6-адрес", value: <NlbVipCell v6AddressId={getByPath<string>(data, "v6_address_id")} /> },
      );
      if (placement === "REGIONAL") {
        items.push({
          label: "Зоны без анонса",
          value:
            drainZones.length > 0 ? (
              refTags(drainZones, "zones", projectId)
            ) : (
              <Typography.Text type="secondary">анонс из всех зон</Typography.Text>
            ),
        });
      }
      // Балансировка между зонами неприменима только зональному размещению
      // (у него зона одна) — при пустом placement_type (EXTERNAL) применима.
      if (placement !== "ZONAL") {
        items.push({
          label: "Балансировка между зонами",
          // Следствие, а не ответ: читателю важно, КУДА уходит трафик, а не то,
          // что где-то поднят флаг. Цветом не выделяется — свойство нейтральное,
          // и акцент на нём обесценил бы акцент там, где он значит опасность.
          value: (
            <BoolFact
              value={getByPath<boolean>(data, "cross_zone_enabled")}
              yes="Трафик уходит во все зоны региона"
              no="Трафик остаётся в своей зоне"
              // Тон ОБЪЯВЛЕН обеим сторонам, а не оставлен умолчанию: ни одна
              // из них не требует внимания — это две законные настройки, и
              // выделив любую, мы обесценили бы акцент там, где он значит
              // опасность (канон §5).
              yesTone="neutral"
              noTone="neutral"
            />
          ),
        });
      }
      if (type === "INTERNAL") {
        items.push({
          label: "Группы безопасности VIP",
          value: refTags(getByPath<string[]>(data, "security_group_ids"), "security-groups", projectId),
        });
      }
      items.push(
        { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
        {
          label: "Защита от удаления",
          // Дословный пример правила 6: «Да» здесь не говорит ни что защита
          // включена, ни что удалить нельзя. Акцент — потому что это ровно то
          // свойство, о котором стоит знать до попытки удаления.
          value: (
            <BoolFact
              value={getByPath<boolean>(data, "deletion_protection")}
              yes="Удаление запрещено"
              no="Удаление разрешено"
              yesTone="good"
              yesGlyph="lock"
              noTone="attention"
              noGlyph="unlock"
            />
          ),
        },
      );
      return items;
    },
    // «Целевые группы» — ЕДИНСТВЕННОЕ, что у карточки балансировщика своё:
    // связь идёт через листенер и одним `filterField` не выражается, поэтому
    // связанным табом реестра (`spec.related`) её не подать. Прежде ради неё
    // держались две конструкции — своя копия оболочки с пропом `extraTabs` и
    // отдельная страница-обёртка балансировщика, — при том что общая оболочка
    // умеет ровно это через реестр расширений.
    //
    // Порядок вкладок при этом стал ОБЩИМ: доменные табы идут после связанных
    // («Обзор» → «Листенеры» → «Целевые группы» → «Операции» → «JSON»), как у
    // правил группы безопасности и маршрутов таблицы. Прежде проп ставил их
    // перед связанными — то есть у балансировщика вкладки шли в своём порядке,
    // и это читалось как другое место продукта.
    extraTabs: ({ data, projectId }): DetailTab[] => [
      {
        id: "target-groups",
        label: "Целевые группы",
        render: () => <LbTargetGroupsTab lbId={getByPath<string>(data, "id") ?? ""} projectId={projectId} />,
      },
    ],
  },

  listeners: {
    overviewExtra: ({ data }) => [
      {
        label: "Балансировщик",
        value: (
          <RefNameLink specId="load-balancers" refId={getByPath<string>(data, "load_balancer_id")} maxChars={42} />
        ),
      },
      { label: "Протокол", value: code(getByPath<string>(data, "protocol")) },
      { label: "Порт", value: code(getByPath<number>(data, "port")) },
      // Строка «Порт на цели» снята (#512): она читала `target_port`, чьи номер и
      // имя у сообщения `Listener` зарезервированы. Край такого поля не отдаёт
      // никогда, поэтому строка показывала прочерк ВСЕГДА — и прочерк на карточке
      // читается как «у слушателя это не задано», а не как «такого у слушателя
      // нет». Порт на цели задаётся составом целевой группы.
      // Целевая группа листенера: привязка перешла сюда со снятых глаголов
      // балансировщика (:attachTargetGroup / :detachTargetGroup). Строка одна, и
      // групп тоже одна: на текущем шаге контракта `target_group_id` и
      // `default_target_group_id` — два имени ОДНОЙ ссылки (владелец отдаёт в них
      // одно значение). Показывается идущее вперёд имя; вторую строку заводить
      // нельзя — она читалась бы как вторая группа.
      {
        label: "Целевая группа",
        value: (
          <RefNameLink
            specId="target-groups"
            refId={getByPath<string>(data, "target_group_id")}
            maxChars={42}
            copy={false}
          />
        ),
        copy: getByPath<string>(data, "target_group_id") ?? undefined,
      },
      // Порт бэкенда — эхо TargetGroup.port, а не frontend-порт листенера.
      // Ноль в контракте означает «ни одна группа не резолвится», а не номер
      // порта, поэтому показывается прочерком.
      { label: "Порт бэкенда", value: code(getByPath<number>(data, "resolved_backend_port") || "") },
      // Подстатус: листенер бывает объявлен и ACTIVE, а обслуживать его некому
      // (целевой группы нет или ссылка повисла). Из строки «Статус» это не видно.
      { label: "Состояние конфигурации", value: substatusTag(getByPath<string>(data, "substatus")) },
      { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
    ],
  },

  "target-groups": {
    overviewExtra: ({ data }) => [
      {
        // Тот же вид, что у балансировщика: один предмет — одна ссылка. Здесь
        // регион показывался плоским текстом, а строкой выше на карточке
        // балансировщика — иначе; со стороны это читается как два разных поля.
        label: "Регион",
        value: <RefNameLink specId="regions" refId={getByPath<string>(data, "region_id")} maxChars={42} copy={false} />,
        copy: getByPath<string>(data, "region_id") ?? undefined,
      },
      // Единственный backend-порт группы: именно его листенер переотражает в
      // `resolved_backend_port`, и от него же наследуется порт пробы.
      { label: "Порт бэкенда", value: code(getByPath<number>(data, "port")) },
      // Duration приходит строкой секунд с хвостовым «s» («300s») — своей
      // единицы подпись не называет, иначе она противоречила бы значению.
      { label: "Время вывода из-под нагрузки", value: code(getByPath<string>(data, "deregistration_delay")) },
      { label: "Медленный старт", value: code(getByPath<string>(data, "slow_start")) },
      // У пробы нет имени (оно снято с контракта: HealthCheck — встроенный
      // объект-значение, а не адресуемый ресурс). Содержательны выбранная ветвь
      // (tcp|http|https|grpc) и разрешённый порт, а не идентичность.
      { label: "Проверка состояния", value: code(healthCheckSummary(data)) },
      { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
    ],
    // Управление backend-таргетами (add/remove через :addTargets/:removeTargets)
    // прямо в блоке «Обзор».
    overviewBelow: ({ data, projectId }) => (
      <TargetsManager
        targetGroupId={getByPath<string>(data, "id") ?? ""}
        projectId={projectId}
        targets={getByPath<Target[]>(data, "targets") ?? []}
      />
    ),
  },
};

// Кладём своё в ОБЩИЙ реестр расширений — тот самый, у которого общая оболочка
// карточки спрашивает расширение по `spec.id`. Ключи раздела (`load-balancers`,
// `listeners`, `target-groups`) в базовом наборе `@shared` отсутствуют, так что
// перекрывать здесь нечего: это дополнение, а не подмена.
for (const [specId, ext] of Object.entries(NLB_DETAIL_EXTENSIONS)) {
  registerDetailExtension(specId, ext);
}
