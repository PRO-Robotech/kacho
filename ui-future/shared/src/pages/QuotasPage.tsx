// «Мои квоты» — витрина пределов арендатора (#364).
//
// Отказ на пределе без витрины неотличим для арендатора от сбоя платформы, и
// каждый такой отказ становится обращением в поддержку. Страница отвечает на три
// вопроса упёршегося: каков предел · сколько занято · КТО этот предел задал.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПЕРЕЧЕНЬ ВЛАДЕЛЬЦЕВ ОБЪЯВЛЕН, А НЕ ВЫВЕДЕН
//
// Общий контракт `quota.v1` один, но подаёт его КАЖДЫЙ владелец сам, своим
// путём. Спросить «все домены» нельзя: у того, кто чтения не завёл, этого пути
// нет, и запрос вернулся бы отказом, который пришлось бы глотать — а глотать
// отказ значит завести контроль, который не отказывает никогда.
//
// Поэтому владельцы перечислены, и перечень называет СОСТОЯНИЕ: сегодня чтение
// подаёт один. Заведёт следующий — одна строка ниже, и его виды появятся сами:
// страница показывает то, что приехало, и не знает закрытого списка видов.
//
// ЧТО СТРАНИЦА НЕ ДЕЛАЕТ. Величины отсюда не меняются: их меняет администратор
// облака своим разделом. Тенантская страница этой ручки не несёт — иначе она
// предлагала бы действие, которое у арендатора отказом и закончится.

import { useMemo } from "react";
import { useParams } from "react-router";
import { useQueries } from "@tanstack/react-query";
import { Alert, Tooltip, Typography } from "antd";
import { DashboardOutlined } from "@ant-design/icons";
import { api } from "@shared/api/client";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { PanelHeader } from "@shared/components/molecules/PanelHeader";
import { ProjectRequiredEmpty } from "@shared/components/molecules/ProjectRequiredEmpty";
import { useBreadcrumb } from "@shared/components/molecules/PageHeaderSlot";
import { ResourceTable, type Column } from "@shared/components/organisms/ResourceTable";
import { quotaRows, type Quota, type QuotaRow } from "@shared/lib/quota-view";

/**
 * Владельцы, подающие чтение квот сегодня.
 *
 * Это ЗАМЕР, а не намерение: путь чтения объявлен и зарегистрирован на крае
 * только у vpc. Остальные домены ведут учёт у себя, но читать его снаружи пока
 * нечем — и написать здесь их пути значило бы обещать витрину, которой нет.
 */
const QUOTA_OWNERS = [{ domain: "vpc", path: "/vpc/v1/quotas" }] as const;

interface QuotasResponse {
  quotas?: Quota[];
}

export function QuotasPage() {
  const { projectId } = useParams();

  // Узел хлебной крошки МЕМОИЗИРУЕТСЯ. Свежий элемент на каждом рендере
  // означал бы новое значение в состоянии слота, то есть новый рендер — и цикл
  // без единого падающего утверждения: суита не доходит до отчёта, а выглядит
  // как зависшая.
  const breadcrumb = useMemo(
    () => (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        <Typography.Text type="secondary">Проект</Typography.Text>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text strong>Квоты</Typography.Text>
      </span>
    ),
    [],
  );
  useBreadcrumb(breadcrumb);

  const results = useQueries({
    queries: QUOTA_OWNERS.map((owner) => ({
      queryKey: ["quotas", owner.domain, projectId ?? null],
      queryFn: () => api.list<QuotasResponse>(owner.path, { projectId: projectId! }),
      enabled: !!projectId,
      staleTime: 10_000,
    })),
  });

  // Отметка последнего обновления каждого запроса — простое выражение в списке
  // зависимостей. Вычислять её ВНУТРИ списка нельзя: правило use-memo требует
  // простых выражений, а вычисление на месте оно прочесть не может.
  const обновлено = results.map((r) => r.dataUpdatedAt).join("|");
  const quotas: Quota[] = useMemo(
    () => results.flatMap((r) => r.data?.quotas ?? []),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [обновлено],
  );
  const rows = useMemo(() => quotaRows(quotas), [quotas]);

  // Заглушка «проект не выбран» — НИЖЕ всех хуков: scope меняется без
  // размонтирования, и ранний выход над хуками менял бы их число между двумя
  // рендерами одного компонента.
  if (!projectId) return <ProjectRequiredEmpty resource="Квоты" />;

  const loading = results.some((r) => r.isLoading);
  const failed = results.filter((r) => r.isError);
  const answered = results.filter((r) => r.isSuccess);

  const columns: Column<QuotaRow>[] = [
    {
      header: "Ресурс",
      className: "font-medium",
      cell: (r) => (
        <Tooltip title={r.kind}>
          <span>{r.label}</span>
        </Tooltip>
      ),
    },
    { header: "Предел", cell: (r) => <span style={{ fontVariantNumeric: "tabular-nums" }}>{r.limit}</span> },
    {
      header: "Занято",
      // Значения нет — называем НОСИТЕЛЯ, а не рисуем прочерк: прочерк на месте
      // живого факта утверждает о ресурсе неправду, а ноль читался бы как
      // «ничего не создано», хотя счёт просто ведётся не здесь.
      cell: (r) =>
        r.used === null ? (
          <Typography.Text type="secondary">{r.carrierLabel}</Typography.Text>
        ) : (
          <span style={{ fontVariantNumeric: "tabular-nums" }}>{r.used}</span>
        ),
    },
    { header: "Кто задал предел", cell: (r) => <Typography.Text type="secondary">{r.source}</Typography.Text> },
  ];

  return (
    <div
      className="kc-surface"
      style={{ padding: 20, height: "100%", overflow: "hidden", display: "flex", flexDirection: "column" }}
    >
      <div style={{ flexShrink: 0, marginBottom: 12 }}>
        <PanelHeader
          icon={<DashboardOutlined />}
          eyebrow="Проект"
          title="Квоты"
          right={
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              Величины задаёт администратор облака
            </Typography.Text>
          }
        />
      </div>

      {failed.length > 0 && (
        <Alert
          type="error"
          showIcon
          style={{ flexShrink: 0, marginBottom: 12 }}
          message={`Пределы не прочитаны: ${failed.length} из ${QUOTA_OWNERS.length}`}
          description={
            // Отказ называется отказом. Молчаливый пропуск сделал бы неполную
            // витрину неотличимой от полной — и упёршийся решил бы, что предела
            // на его ресурс нет вовсе.
            <ErrorResult error={failed[0].error} />
          }
        />
      )}

      {!loading && failed.length === 0 && answered.length > 0 && quotas.length === 0 && (
        <Alert
          type="warning"
          showIcon
          style={{ flexShrink: 0, marginBottom: 12 }}
          message="Ответ не назвал ни одного предела"
          description={
            // Контракт обещает полный набор видов всегда — проект, ничего не
            // создавший, получает их с нулями. Пустой ответ означает, что что-то
            // не так с чтением, и выдать его за «ограничений нет» — солгать.
            "Так быть не должно: полный набор ограничений приходит даже проекту, в котором ещё ничего не создано. " +
            "Обратитесь к администратору облака."
          }
        />
      )}

      <div style={{ flex: 1, minHeight: 0, minWidth: 0 }}>
        <ResourceTable
          rows={rows}
          loading={loading && rows.length === 0}
          rowKey={(r) => r.kind}
          columns={columns}
          // Порядок задан здесь (по имени вида) и устойчив: ответ порядка не
          // обещает, а страница поллится — показанный «как пришёл» список
          // переставлялся бы под курсором читателя.
          empty="Ограничения не прочитаны"
        />
      </div>
    </div>
  );
}
