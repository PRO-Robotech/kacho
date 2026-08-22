// CidrTableSection — секция CIDR-блоков одного семейства (v4/v6): шапка с
// бейджем семейства, таблица [CIDR-блок | ⌫] и строка ввода в подвале.
//
// Единственный вид для ВСЕХ наборов CIDR консоли: блоки подсети, объявленный
// супернет сети, состав набора префиксов и диапазоны пула адресов. Механика у
// всех четырёх одна (add и remove суть РАЗНЫЕ глаголы края, каждое действие
// уходит сразу своим RPC, без batch-save), а вид был разный — то таблица, то
// карточка с чипами: два вида одного предмета читаются как два предмета.
//
// Своё у ресурса — путь глагола, имена полей семейства, тексты и, у пула,
// добавочные поля тела: это объявляет ВЛАДЕЛЕЦ, рядом со своим путём. Всё
// остальное — здесь, в одном месте.
//
// Край запрещает менять эти блоки правкой (immutable after Create) — только
// парой глаголов «добавить»/«снять». Как они названы и чем отвечают, знает
// владелец: у подсети, сети и набора префиксов — операцией, у пула адресов —
// собой. Секция умеет оба исхода: операции в ответе нет — обновляем кэш сразу.
import { useState } from "react";
import { DETAIL_CONTENT_WIDTH, DetailSurface } from "@shared/components/organisms/DetailShell";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button, Input, Spin, Tag, Tooltip } from "antd";
import { DeleteOutlined, LoadingOutlined, LockOutlined, PlusOutlined } from "@ant-design/icons";
import { api } from "@shared/api/client";
import { OperationToastWatcher } from "@shared/components/molecules/OperationToastWatcher";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { toast } from "@shared/lib/toast";
import { errorText } from "@shared/lib/error-presentation";
import {
  EDITOR_ACTIONS_WIDTH,
  MONO_FONT,
  editorAddButtonStyle,
  editorAddInputStyle,
  editorAddRowStyle,
  editorEmptyStyle,
  editorIconButtonStyle,
  editorRowStyle,
  editorFirstRowStyle,
  editorBodyStyle,
  editorValueCellStyle,
} from "@shared/components/organisms/form/editor-surface";

export type CidrKind = "v4" | "v6";

// Имена полей семейства задаёт ВЛАДЕЛЕЦ ресурса, а не эта секция.
//
// Здесь стояла общая пара `ipv4_cidr_blocks`/`ipv6_cidr_blocks` — верная для
// подсети и сети и НЕВЕРНАЯ для набора префиксов, чьи глаголы принимают
// `v4_cidr_blocks`/`v6_cidr_blocks`. Умолчание в таком месте — худший из
// исходов: край разбирает тело, отбрасывая неизвестные ключи МОЛЧА, поэтому
// действие вернуло бы успех, ничего не изменив. Поэтому пара обязательна и
// объявляется у ресурса, рядом с его же путём глагола.
export type CidrBlockFields = Record<CidrKind, string>;

/** Пара имён для ресурсов, чьи глаголы ключуют семейство полным словом. */
export const IP_PREFIXED_BLOCK_FIELDS: CidrBlockFields = {
  v4: "ipv4_cidr_blocks",
  v6: "ipv6_cidr_blocks",
};

export function validateCidr(kind: CidrKind, cidr: string, prefixExample: string): string | null {
  if (!cidr) return "Введите CIDR.";
  if (!cidr.includes("/")) return `CIDR должен содержать префикс (например ${prefixExample}).`;
  if (kind === "v6" && !cidr.includes(":")) return "Похоже не на IPv6-адрес.";
  return null;
}

export interface CidrTableSectionProps {
  /** Путь глагола края. Строит его ВЛАДЕЛЕЦ ресурса, а не эта секция: голова
   *  пути обязана оставаться статически видимой пробе поверхности API
   *  (`lib/api-path-surface`), которая резолвит её в константу файла. Спрятав
   *  путь за проп, секция вывела бы оба ресурса из-под её наблюдения. */
  actionPath: (verb: "add" | "remove") => string;
  /** Как ВЛАДЕЛЕЦ называет поля семейства в теле глагола. Обязателен: умолчание
   *  здесь дало бы успешный ответ на действие, ничего не изменившее. */
  blockFields: CidrBlockFields;
  /** Префикс ключа кэша владельца: обновляется и карточка, и список. */
  invalidateKey: string;
  /** Ещё ключи кэша, которые обязано обновить это действие. Нужен там, где на
   *  тех же блоках стоит отдельный виджет (заполненность пула адресов): его
   *  запрос лежит под своим ключом, и без него число на экране пережило бы
   *  снятие блока, из которого считалось. */
  alsoInvalidate?: readonly (readonly unknown[])[];
  /** Поля тела глагола сверх набора блоков — их называет ВЛАДЕЛЕЦ ресурса.
   *  Пул адресов повторяет свой id в теле, и это его контракт, а не общее
   *  правило: подставить его здесь всем значило бы слать чужому глаголу поле,
   *  которого он не знает, — а неизвестный ключ край отбрасывает МОЛЧА. */
  extraBody?: Record<string, unknown>;
  kind: CidrKind;
  /** Изменяемые блоки (уходят через глаголы). */
  blocks: string[];
  /** Заголовок секции: «CIDR» у подсети, «Супернет» у сети. */
  title: string;
  /** Пример префикса в тексте отказа валидации: «/24» у подсети, «/16» у сети. */
  prefixExample: string;
  placeholder: string;
  /** Как назван блок в тексте операции: «CIDR» / «супернет-блока». */
  opNoun: string;
  /** Как назван набор в тексте отказа: «CIDR» / «супернет». */
  errNoun: string;
  emptyText: string;
  /** Неизменяемый основной блок — показывается запертым, глаголами не проходит. */
  primary?: string;
  /** Подпись у замка основного блока. */
  primaryHint?: string;
}

export function CidrTableSection({
  actionPath,
  blockFields,
  invalidateKey,
  alsoInvalidate,
  extraBody,
  kind,
  blocks,
  title,
  prefixExample,
  placeholder,
  opNoun,
  errNoun,
  emptyText,
  primary,
  primaryHint,
}: CidrTableSectionProps) {
  const qc = useQueryClient();
  const [draft, setDraft] = useState("");
  const [opId, setOpId] = useState<string | null>(null);
  const [opTitle, setOpTitle] = useState("");
  const [pendingCidr, setPendingCidr] = useState<string | null>(null);
  /**
   * Отказ ВВОДА — рядом с полем, а не всплывающим сообщением.
   *
   * Прежде отказ показывался всплывающим сообщением: оно появляется в углу
   * экрана, живёт несколько секунд и исчезает, а негодная строка остаётся в
   * поле без единого признака того, чем именно она негодна. Человек, набравший
   * `10.0.1.0` без префикса, перечитывает своё значение и не видит претензии —
   * она уже уехала. Причина отказа обязана стоять там, где её читают перед
   * повторным нажатием, и жить, пока значение не исправлено.
   *
   * Отказ КРАЯ (снятие блока, в котором есть выданные адреса) остаётся
   * всплывающим: он относится к строке набора, а не к вводу, и поля, рядом с
   * которым его показать, у него нет.
   */
  const [refusal, setRefusal] = useState<string | null>(null);

  const family = kind === "v4" ? "IPv4" : "IPv6";
  const field = blockFields[kind];

  /** Обновить всё, что показывает эти блоки: свой ресурс и названные соседи. */
  const invalidateAll = () => {
    void qc.invalidateQueries({ queryKey: [invalidateKey] });
    for (const key of alsoInvalidate ?? []) void qc.invalidateQueries({ queryKey: [...key] });
  };

  const mutate = useMutation({
    mutationFn: (params: { verb: "add" | "remove"; cidr: string }) =>
      api.action(actionPath(params.verb), { ...extraBody, [field]: [params.cidr] }),
    onSuccess: (resp, vars) => {
      const id = extractOperationId(resp);
      if (id) {
        setOpTitle(`${vars.verb === "add" ? "Добавление" : "Удаление"} ${family} ${opNoun} ${vars.cidr}`);
        setOpId(id);
        setPendingCidr(vars.cidr);
      } else {
        // Широкий prefix-инвалидейт: матчит и карточку (shell-detail), и список
        // — узкие ключи не совпадали с ключом ResourceShell, и карточка
        // показывала прежний набор.
        invalidateAll();
        setPendingCidr(null);
      }
    },
    onError: (err, vars) => {
      const m = errorText(err);
      toast.error(`${family} ${errNoun} ${vars.verb === "add" ? "добавление" : "удаление"}: ${m}`);
      setPendingCidr(null);
    },
  });

  const inputDisabled = mutate.isPending || opId !== null;

  const onAdd = () => {
    const cidr = draft.trim();
    const verr = validateCidr(kind, cidr, prefixExample);
    if (verr) {
      setRefusal(verr);
      return;
    }
    if (blocks.includes(cidr)) {
      setRefusal("Этот CIDR уже добавлен.");
      return;
    }
    setRefusal(null);
    setPendingCidr(cidr);
    mutate.mutate({ verb: "add", cidr });
    setDraft("");
  };

  const onRemove = (cidr: string) => {
    if (inputDisabled) return;
    setPendingCidr(cidr);
    mutate.mutate({ verb: "remove", cidr });
  };

  // Семейство адресов стоит В ЗАГОЛОВКЕ, а не отдельной плиткой слева.
  //
  // Плитка его и несла, и когда она ушла вместе со своей шапкой, две секции
  // подряд стали называться одинаково — «CIDR» и «CIDR», — то есть перестали
  // различаться вовсе. Заголовок, не отличающий секцию от соседней, не
  // выполняет своей работы; поэтому семейство переехало в него, а не пропало.
  const heading = `${family} ${title}`;

  // Примечание справа отвечает на «зачем эта секция», а не повторяет заголовок.
  const note = primary
    ? "Основной якорь и дополнительные блоки"
    : family === "IPv6"
      ? "Опционально"
      : "Адресное пространство";

  return (
    <div style={{ marginTop: 24, maxWidth: DETAIL_CONTENT_WIDTH }}>
      <DetailSurface title={heading} note={note}>

      <div style={editorBodyStyle}>
        <table className="w-full" style={{ tableLayout: "fixed", borderCollapse: "collapse" }}>
          <colgroup>
            <col style={{ width: `calc(100% - ${EDITOR_ACTIONS_WIDTH}px)` }} />
            <col style={{ width: EDITOR_ACTIONS_WIDTH }} />
          </colgroup>
      {/* Шапки колонки нет: в секции один столбец значений, и подпись «CIDR-блок»
          повторяла заголовок секции («IPv4 CIDR») другими словами. Строка,
          которая ничего не добавляет к сказанному выше, занимает высоту и
          отодвигает сами значения. */}
          <tbody>
            {!primary && blocks.length === 0 && (
              <tr>
                {/* Ячейка `minHeight` не читает — высоту задаёт `height`,
                    который для ячейки и есть минимум. */}
                <td colSpan={2} style={{ ...editorEmptyStyle, height: 72 }}>
                  {emptyText}
                </td>
              </tr>
            )}
            {primary && (
              <tr className="kc-kv-row" style={editorFirstRowStyle}>
                <td style={editorValueCellStyle}>
                  {primary}{" "}
                  <Tag style={{ marginLeft: 6, fontFamily: MONO_FONT, fontSize: 10, fontWeight: 540 }}>основной</Tag>
                </td>
                <td style={{ textAlign: "center", verticalAlign: "middle" }}>
                  <Tooltip title={primaryHint}>
                    <LockOutlined style={{ color: "var(--kc-text-tertiary)", fontSize: 13 }} />
                  </Tooltip>
                </td>
              </tr>
            )}
            {blocks.map((cidr, i) => {
              const busy = pendingCidr === cidr && (mutate.isPending || opId !== null);
              return (
                <tr key={i} className="kc-kv-row" style={i === 0 && !primary ? editorFirstRowStyle : editorRowStyle}>
                  <td style={editorValueCellStyle}>{cidr}</td>
                  <td style={{ textAlign: "center", verticalAlign: "middle" }}>
                    {busy ? (
                      <Spin indicator={<LoadingOutlined style={{ fontSize: 12 }} spin />} />
                    ) : (
                      <Button
                        type="text"
                        danger
                        icon={<DeleteOutlined />}
                        aria-label="Удалить CIDR"
                        onClick={() => onRemove(cidr)}
                        disabled={inputDisabled}
                        style={editorIconButtonStyle}
                      />
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>

        {/* Строка добавления — вне таблицы: она не строка набора, а действие над
            ним. Внутри <tfoot> её поле наследовало бы ширины колонок и ломалось
            бы вместе с ними. */}
        <div style={editorAddRowStyle}>
          <Input
            value={draft}
            /* Набор нового значения снимает прежнюю претензию: она была про то,
               что стёрто, и держать её значило бы обвинять человека в том, чего
               в поле уже нет. */
            onChange={(e) => {
              setDraft(e.target.value);
              if (refusal) setRefusal(null);
            }}
            placeholder={placeholder}
            disabled={inputDisabled}
            status={refusal ? "error" : undefined}
            aria-invalid={refusal ? true : undefined}
            /* Цвет границы задан здесь, а не оставлен `status`: рамка — то
               единственное, что связывает претензию с полем, и она обязана
               держаться токеном палитры, а не производным цветом виджета. */
            style={refusal ? { ...editorAddInputStyle, borderColor: "var(--kc-danger)" } : editorAddInputStyle}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                onAdd();
              }
            }}
          />
          <Button
            icon={<PlusOutlined />}
            onClick={onAdd}
            disabled={!draft.trim() || inputDisabled}
            style={editorAddButtonStyle}
          >
            Добавить
          </Button>
        </div>

        {refusal && (
          // Претензия названа словами: чего не хватает во ВВЕДЁННОМ значении, а
          // не «неверное значение» — второе не подсказывает, что править.
          <div
            role="alert"
            style={{
              padding: "0 10px 10px",
              fontSize: 11,
              lineHeight: 1.35,
              color: "var(--kc-danger)",
            }}
          >
            {refusal}
          </div>
        )}
      </div>

      <OperationToastWatcher
        opId={opId}
        title={opTitle}
        onDone={() => {
          setOpId(null);
          setPendingCidr(null);
          invalidateAll();
        }}
      />
      </DetailSurface>
    </div>
  );
}
