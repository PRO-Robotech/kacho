// Пустой список ресурса: почему смотреть не на что и каков следующий шаг.
//
// ─────────────────────────────────────────────────────────────────────────────
// ОДИН СТИЛЬ С ЭКРАНОМ «РАЗДЕЛ ВРЕМЕННО НЕДОСТУПЕН» (решение владельца)
//
// Оболочка была общей (`StatePanel`) и раньше, но ВИД расходился по двум местам,
// и этого хватало, чтобы экраны читались приехавшими из разных продуктов:
//
//   было                            стало
//   плитка 96×96 с глифом ресурса   линейный рисунок из токенов, штрих 1.25
//   коробка с заливкой и ссылками   перечень тем строкой, под линией
//
// Предмет у обоих экранов один — «смотреть не на что, вот следующий шаг», —
// поэтому и вид один. Отличается по делу только содержимое: рисунок, слова,
// действия.

import { Button } from "antd";
import { StatePanel } from "@shared/components/molecules/StatePanel";
import { PlusOutlined } from "@ant-design/icons";
import { ResourceEmptyArt } from "./ResourceEmptyArt";
import { ART_BY_SPEC } from "./art";
import type { ResourceSpec } from "@shared/lib/resource-registry";

interface Props {
  spec: ResourceSpec;
  onCreate: () => void;
  /** Переопределение текста кнопки (по умолчанию «Создать <singular>»). */
  createLabel?: string;
}

export function ResourceEmptyState({ spec, onCreate, createLabel }: Props) {
  const copy = spec.emptyState;
  // Короткое «Создать»: предмет назван заголовком экрана строкой выше
  // («Создайте первую подсеть»), и повторять его на кнопке незачем.
  const label = createLabel ?? "Создать";
  const docs = copy?.docs ?? [];
  return (
    <StatePanel
      role="status"
      art={(() => {
        // Тематический рисунок ресурса, а общий — только там, где схемы у
        // предмета нет (список проектов, пользователей). Промах по карте
        // означает именно это, а не поломку, поэтому запасной путь тихий.
        const Art = ART_BY_SPEC[spec.id] ?? ResourceEmptyArt;
        return <Art />;
      })()}
      title={copy?.title ?? `Создайте первый ресурс «${spec.singular.toLowerCase()}»`}
      description={copy?.body}
      actions={
        // Призыв создать — только там, где глагол создания есть. Репозиторий
        // материализуется `docker push`, тип диска и тип машины заводит
        // администратор облака: кнопка над ними обещала бы возможность, которой
        // нет, — нажавший получал отказ края там, где консоль позвала его сама.
        spec.ops.create ? (
          <Button type="primary" icon={<PlusOutlined />} onClick={onCreate}>
            {label}
          </Button>
        ) : undefined
      }
      footer={
        docs.length > 0 ? (
          <div
            style={{
              display: "flex",
              alignItems: "baseline",
              justifyContent: "center",
              flexWrap: "wrap",
              gap: "6px 12px",
              width: "100%",
              paddingTop: 16,
              borderTop: "1px solid var(--kc-border)",
            }}
          >
            <span
              style={{
                color: "var(--kc-text-tertiary)",
                fontSize: 9,
                fontWeight: 700,
                letterSpacing: "0.12em",
                textTransform: "uppercase",
              }}
            >
              Документация
            </span>
            {/* ТЕМЫ, А НЕ ССЫЛКИ — и это не упрощение вида, а снятие ложного
                обещания. Адресов у документации в дереве нет ни одного: все
                объявления несут `href: "#"` (замер по обоим реестрам и по карте
                каркаса — 61 объявление, живых 0). Ссылка, ведущая на ту же
                страницу, обещает переход, которого не существует, и обнаруживает
                это только кликом. Перечень тем без перехода честен: он называет,
                что читать, и ничего не обещает.

                Появится адрес сайта документации — темы станут ссылками здесь,
                в одном месте, а не в шестидесяти одном объявлении. */}
            {docs.map((doc) => (
              <span key={doc} style={{ color: "var(--kc-text-secondary)", fontSize: 13 }}>
                {doc}
              </span>
            ))}
          </div>
        ) : undefined
      }
    />
  );
}
