// registerExtensions — регистрация доменных расширений карточки registry-remote.
// Импортируется side-effect'ом входной точкой модуля (RegistryPage), поэтому
// расширения подключены на старте бандла, до рендера карточек.
//
// Оболочка карточки остаётся app-agnostic: доменное содержимое реестра — адрес
// для `docker login`/`push`, размещение (REG-1 F4, REGIONAL-anycast), видимость
// репозиториев по умолчанию (REG-1 F5), число репозиториев и статус, а также
// действие шапки «Управление доступом» — инжектится ЗДЕСЬ через
// `registerDetailExtension`. Строки репозитория (класс исчезаемости F7,
// видимость F5, число тегов и агрегатный размер) — тем же порядком.
//
// Здесь стоял ВТОРОЙ реестр расширений — собственная копия
// `ResourceDetailExtensions` этого модуля (142 строки): свои объявления типов
// `DescItem`/`DetailExtCtx`/`DetailExtension`, свой `detailExtension` и свои
// помощники показа значения. Общий реестр параметр для доменного содержимого уже
// имел (`registerDetailExtension`), поэтому копия ничего не давала и только
// отставала: `String(v)` вместо `displayText(v)` показывает `[object Object]` на
// вложенном поле, а свой `<span>` с моноширинным шрифтом — второй вид того же
// значения, которое в строке свойств рисует `MonoValue`.

import { Button } from "antd";
import { SafetyCertificateOutlined } from "@ant-design/icons";

import { registerDetailExtension } from "@shared/components/organisms/ResourceDetailExtensions";
import { MonoValue } from "@shared/components/atoms/CopyableId/MonoValue";
import { StatusBadge } from "@shared/components/atoms/StatusBadge";
import { PlacementAnchor } from "@shared/components/molecules/PlacementAnchor";
import { getByPath } from "@shared/lib/resource-registry";
import { displayText } from "@shared/lib/display-text";
import { formatBytes } from "@shared/lib/bytes";

import { RepositoryLifecycleTag } from "@shared/components/atoms/RepositoryLifecycleTag";
import { VisibilityTag } from "@shared/components/atoms/VisibilityTag";

/** Значение строки свойств словами; пусто — прочерк, как у «Описания» Обзора. */
function text(v: unknown): string {
  return displayText(v) || "—";
}

registerDetailExtension("registries", {
  overviewExtra: ({ data }) => {
    // Адрес реестра ПЕРЕНОСЯТ в чужое поле — в команду `docker login`/`push`, —
    // поэтому копирование у этой строки объявлено, и рисует его общий значок
    // строки, а не своя кнопка внутри значения.
    const endpoint = getByPath<string>(data, "endpoint") ?? "";
    return [
      { label: "Адрес", value: <MonoValue value={endpoint} />, copy: endpoint || undefined },
      // ОДНА строка размещения вместо двух. Прежде их было две: «Регион» с
      // плоским идентификатором и «Размещение» с сырым токеном `REGIONAL` — то
      // есть машинное слово рядом с тем же самым фактом, уже названным строкой
      // выше. Ветку ZONAL/REGIONAL рисует единственный `PlacementAnchor`: вид
      // размещения он отдельным словом не называет — вид и есть тип ресурса, на
      // который ведёт ссылка (правило 2 канона консоли).
      {
        label: "Размещение",
        value: <PlacementAnchor row={data} maxChars={32} />,
        // Копируется ИДЕНТИФИКАТОР якоря, а не его имя: имя меняется, координата
        // размещения — нет (ban #15).
        copy: getByPath<string>(data, "region_id") || undefined,
      },
      {
        label: "Видимость репозиториев по умолчанию",
        value: <VisibilityTag value={getByPath<string>(data, "default_repository_visibility")} />,
      },
      { label: "Репозиториев", value: text(getByPath<number>(data, "repository_count") ?? 0) },
      { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
    ];
  },
  // «Управление доступом» — доступ к реестру = registry-scoped Role, привязанная
  // на ПРОЕКТЕ реестра (уровни scope — только CLUSTER/ACCOUNT/PROJECT, отдельного
  // per-registry-object scope нет). Кнопка ведёт в IAM-remote к созданию
  // AccessBinding на проекте; форму IAM cross-remote НЕ импортируем.
  headerActions: ({ projectId, navigate }) =>
    projectId ? (
      <Button
        icon={<SafetyCertificateOutlined />}
        onClick={() => navigate(`/projects/${projectId}/iam/access-bindings/create`)}
      >
        Управление доступом
      </Button>
    ) : null,
});

registerDetailExtension("repositories", {
  overviewExtra: ({ data }) => [
    { label: "Класс", value: <RepositoryLifecycleTag value={getByPath<string>(data, "lifecycle")} /> },
    { label: "Видимость", value: <VisibilityTag value={getByPath<string>(data, "visibility")} /> },
    { label: "Тегов", value: text(getByPath<number>(data, "tag_count") ?? 0) },
    // `formatBytes` сам отвечает прочерком на пустом и на не-числе, поэтому
    // второй проверки здесь нет: она была бы вторым правилом о том же значении.
    { label: "Размер", value: formatBytes(getByPath<unknown>(data, "size_bytes")) },
  ],
});
