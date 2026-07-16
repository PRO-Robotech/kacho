// DashboardPage — root экран /dashboard. Разводная страница: плашки опубликованных
// сервисов (модулей) — сейчас VPC и Compute. Клик по плашке → вход в модуль
// (сайдбар переключает набор ссылок на этот модуль, см. ServiceSidebar).
//
// Уровни контекста (выбираются pill'ами в шапке — BreadcrumbSelector):
//   • project выбран    → counts по project + клик → landing модуля в этом project
//   • ничего не выбрано → "—" + CTA «Перейти в IAM»

import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { useQueries } from "@tanstack/react-query";
import { Card, Empty, Statistic, Typography, Space, Button, Row, Col, Alert, Tooltip } from "antd";
import { ArrowRightOutlined, FolderOpenOutlined, AppstoreOutlined, LockOutlined } from "@ant-design/icons";
import { useBreadcrumb, useHeaderRight, usePageTitle } from "@shared/components/molecules/PageHeaderSlot";
import { api } from "@shared/api/client";
import { useContext } from "@shared/lib/context-store";
import { SERVICE_MODULES, type ServiceModule } from "@shared/lib/service-modules";

type CountMap = Record<string, number | null>;

/** Counts по stat-метрикам модуля для выбранного scope (project либо account).
 *
 * scopeKey = "project_id" (по умолчанию) — для VPC/Compute/NLB stats (per-project).
 * scopeKey = "account_id" — для IAM stats (account-level, project_id=* ломал AuthZ:
 * "no path: unscoped resource", subject=account:*, KAC-175-followup).
 * scopeKey = "" — list-all без scope (для stats где endpoint не требует фильтра,
 * напр. /iam/v1/accounts).
 */
function useModuleCounts(module: ServiceModule, scopeId: string | null, scopeKey: string = "project_id"): CountMap {
  const enabled = scopeKey === "" || scopeId != null;
  const results = useQueries({
    queries: module.stats.map((stat) => ({
      queryKey: ["dash", module.key, stat.key, scopeKey, scopeId],
      enabled,
      refetchInterval: 15_000,
      queryFn: async () => {
        const query: Record<string, string> = { pageSize: "1000" };
        if (scopeKey !== "" && scopeId != null) {
          query[scopeKey] = scopeId;
        }
        const l = await api.list<Record<string, unknown[] | undefined>>(stat.listPath, query);
        return l[stat.payloadKey]?.length ?? 0;
      },
    })),
  });
  const out: CountMap = {};
  module.stats.forEach((stat, i) => {
    out[stat.key] = enabled ? (results[i].data ?? null) : null;
  });
  return out;
}

export function DashboardPage() {
  const ctx = useContext((s) => s);
  const navigate = useNavigate();

  const projectId = ctx.project?.id ?? null;
  const accountId = ctx.account?.id ?? null;

  // Counts для каждого модуля. Lookup ПО key, не по индексу — иначе порядок
  // в SERVICE_MODULES (vpc/compute/nlb/iam) ломает binding и iam счётчики
  // показывают NLB-данные (KAC-171 регресс предотвращён).
  // IAM stats — account-level: используем accountId как scope, scopeKey="" чтобы
  // запросы шли БЕЗ project_id (раньше project_id="*" вызывал AuthZ "no path:
  // unscoped resource" — backend требует валидный scope, не wildcard).
  const vpcModule = SERVICE_MODULES.find((m) => m.key === "vpc")!;
  const computeModule = SERVICE_MODULES.find((m) => m.key === "compute")!;
  const nlbModule = SERVICE_MODULES.find((m) => m.key === "nlb");
  const iamModule = SERVICE_MODULES.find((m) => m.key === "iam")!;
  const vpcCounts = useModuleCounts(vpcModule, projectId);
  const computeCounts = useModuleCounts(computeModule, projectId);
  const nlbCounts = useModuleCounts(nlbModule ?? vpcModule, nlbModule ? projectId : null);
  // IAM: scopeKey="" → list без фильтра (accounts API сам делает per-user filter).
  // Если в будущем roles/projects/users потребуют account_id — переключиться на
  // useModuleCounts(iamModule, accountId, "account_id"). Сейчас accounts работает
  // без scope, остальные iam stats — могут падать 403, но хук не throws (try/catch
  // в queryFn react-query).
  const iamCounts = useModuleCounts(iamModule, accountId ?? "all", "");
  const countsByModule: Record<string, CountMap> = {
    [vpcModule.key]: vpcCounts,
    [computeModule.key]: computeCounts,
    [iamModule.key]: iamCounts,
    ...(nlbModule ? { [nlbModule.key]: nlbCounts } : {}),
  };

  useBreadcrumb(useMemo(() => <Typography.Text strong>Все сервисы</Typography.Text>, []));
  useHeaderRight(useMemo(() => null, []));
  usePageTitle(null);

  // Плашка кликабельна только если landing вернул реальный route. Project-scoped
  // модуль (VPC/Compute) без выбранного project → landing=null → disabled-плашка.
  const tileDisabled = (m: ServiceModule) => m.landing(projectId, accountId) == null;

  const openModule = (m: ServiceModule) => {
    const target = m.landing(projectId, accountId);
    if (target == null) return; // no-op для disabled-плашки
    navigate(target);
  };

  const caption = (() => {
    if (ctx.project) return `Проект: ${ctx.project.name || ctx.project.id}`;
    if (ctx.account)
      return `Аккаунт: ${ctx.account.name || ctx.account.id} — выберите проект чтобы перейти к ресурсам.`;
    return "Контекст не выбран — выберите Account и Project в шапке. IAM-блок доступен всегда.";
  })();

  // Плашки VPC/Compute требуют Project context. IAM — всегда виден.
  const tilesVisible = true;
  const allEmpty =
    !!ctx.project &&
    SERVICE_MODULES.filter((m) => m.key !== "iam").every((m) =>
      m.stats.every((s) => (countsByModule[m.key]?.[s.key] ?? null) === 0),
    );

  return (
    <div style={{ maxWidth: 1100 }} data-testid="dashboard-page">
      <Space direction="vertical" size={20} style={{ width: "100%" }}>
        <div>
          <Typography.Title level={3} className="t-page-title" style={{ margin: 0 }}>
            Сервисы облака
          </Typography.Title>
          <Typography.Text type="secondary">{caption}</Typography.Text>
        </div>

        {!ctx.account && (
          <Alert
            type="info"
            showIcon
            message="Выберите Account и Project в шапке для просмотра VPC и Compute ресурсов. IAM доступен всегда."
            action={
              <Button
                size="small"
                icon={<ArrowRightOutlined />}
                onClick={() => navigate("/iam/accounts")}
                data-testid="dashboard-go-iam"
              >
                Перейти в IAM
              </Button>
            }
          />
        )}

        {tilesVisible && (
          <Row gutter={[16, 16]}>
            {SERVICE_MODULES.map((m) => {
              const disabled = tileDisabled(m);
              const card = (
                <Card
                  hoverable={!disabled}
                  data-testid={`dashboard-tile-${m.key}`}
                  data-disabled={disabled ? "true" : "false"}
                  onClick={() => openModule(m)}
                  styles={{ body: { padding: 16 } }}
                  style={disabled ? { opacity: 0.55, cursor: "not-allowed" } : { cursor: "pointer" }}
                  title={
                    <Space>
                      <span style={{ color: m.color, fontSize: 16 }}>{m.icon}</span>
                      <span>{m.label}</span>
                    </Space>
                  }
                  extra={disabled ? <LockOutlined /> : <ArrowRightOutlined />}
                >
                  <Typography.Paragraph type="secondary" style={{ marginBottom: 12, fontSize: 12 }}>
                    {m.description}
                  </Typography.Paragraph>
                  {disabled && (
                    <Typography.Text type="warning" style={{ fontSize: 12, display: "block", marginBottom: 8 }}>
                      Выберите проект в шапке, чтобы открыть ресурсы.
                    </Typography.Text>
                  )}
                  <Row gutter={16}>
                    {m.stats.map((s) => (
                      <Col key={s.key} span={Math.floor(24 / m.stats.length)}>
                        <Statistic
                          title={s.label}
                          value={countsByModule[m.key]?.[s.key] ?? "—"}
                          valueStyle={{ fontSize: 22 }}
                        />
                      </Col>
                    ))}
                  </Row>
                </Card>
              );
              return (
                <Col key={m.key} xs={24} sm={24} md={12} lg={12}>
                  {disabled ? <Tooltip title="Выберите проект в селекторе в шапке">{card}</Tooltip> : card}
                </Col>
              );
            })}
          </Row>
        )}

        {allEmpty && (
          <Card>
            <Empty
              image={<FolderOpenOutlined style={{ fontSize: 40, color: "#8b8f99" }} />}
              imageStyle={{ height: 56 }}
              description={
                <Space direction="vertical" size={6}>
                  <Typography.Text strong>В каталоге нет ресурсов</Typography.Text>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    Выберите сервис на плашке выше, чтобы создать первый ресурс.
                  </Typography.Text>
                </Space>
              }
            >
              <Button type="primary" icon={<AppstoreOutlined />} onClick={() => openModule(SERVICE_MODULES[0])}>
                Перейти в {SERVICE_MODULES[0].short}
              </Button>
            </Empty>
          </Card>
        )}
      </Space>
    </div>
  );
}
