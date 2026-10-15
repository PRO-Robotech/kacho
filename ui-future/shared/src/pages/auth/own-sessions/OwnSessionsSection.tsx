// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useCallback, useEffect, useState } from "react";
import { Button, Spin, Tag, Typography } from "antd";
import { type LaneRefusal, laneRefusalOf, loginLane, type OwnSession } from "@shared/api/login-lane";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { type Column, ResourceTable } from "@shared/components/organisms/ResourceTable";
import { useFormToken } from "@shared/hooks/use-form-token";
import { useAnnounce } from "../security/announcer";
import { DangerDialog } from "../security/DangerDialog";
import { Moment } from "../security/moment";
import { SecuritySection } from "../security/SecuritySection";

// Раздел «Активные сессии» экрана «Безопасность» (приёмка F8-S5, Р2–Р5; вид —
// приёмка F8-S7, Р3, Р6, Р8).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО РАЗДЕЛ ДЕЛАЕТ
//
//   • при открытии — ОДНО чтение перечня своих живых сессий; порядок строк —
//     порядок службы, консоль его не переставляет; непустой `nextPageToken` —
//     «Показать ещё», дочитывающее следующую страницу тем же путём;
//   • перечень — ОБЩЕЙ таблицей (F8-S7, Р3; поведение F8-S5 без изменения):
//     описание клиента КАК ПРИСЛАНО (Р3 F8-S5: разбора в «браузер» и «систему»
//     нет; ключа нет — «Устройство не названо»), моменты входа, последнего
//     предъявления способа и срока — в часовом поясе браузера;
//   • текущая — пометкой «Эта сессия» и БЕЗ «Выйти»: текущая завершается
//     выходом, который у консоли уже есть (служба на её снятие отвечает
//     `SESSION_IS_CURRENT`);
//   • «Выйти на всех, кроме этой» — опасное действие: диалог называет
//     последствие (F8-S7, Р6). Других сессий нет — кнопки нет, а раздел так и
//     говорит (Р8).
//
// После КАЖДОЙ попытки снятия — исход областью `role="status"` экрана и
// перечитывание перечня: строка, снятая где-то ещё, уходит из перечня тем же
// ответом, которым служба отказала (`SESSION_NOT_FOUND`).
//
// ОТКАЗЫ — действием существующего перечня (Р5, условие C2): несвежая сессия —
// повышение и ОДИН повтор (клиент полосы, `submit`); прочее — текст дословно.
// Своих ветвей у раздела нет.
//
// ПРИЗНАК ФОРМЫ — один держатель вида `session-end` на раздел (Р4): оба
// снимающих глагола берут его, перечень — нет.

/** Строка исхода снятия (Р2). */
function outcomeText(ended: true | number): string {
  if (ended === true) return "Сессия завершена";
  if (ended === 0) return "Других сессий не было";
  return `Завершено сессий: ${ended}`;
}

/** Тексты раздела (F8-S7, Р6, Р8). */
export const OWN_SESSIONS_TEXT = {
  noOthers: "Других активных сессий нет.",
  endOthersConsequence: "Все другие устройства выйдут из учётной записи.",
} as const;

function deviceOf(session: OwnSession): string {
  return session.userAgent === undefined ? "Устройство не названо" : session.userAgent;
}

const at = (iso: string | undefined) => (iso === undefined ? null : <Moment at={iso} />);

export function OwnSessionsSection({ onCount }: { onCount?: (n: number) => void } = {}) {
  const holder = useFormToken("session-end");
  const announce = useAnnounce();
  const [rows, setRows] = useState<OwnSession[] | null>(null);
  const [nextPageToken, setNextPageToken] = useState("");
  const [listRefusal, setListRefusal] = useState<LaneRefusal | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);
  const [asking, setAsking] = useState(false);

  useEffect(() => {
    if (rows !== null && nextPageToken === "") onCount?.(rows.length);
  }, [rows, nextPageToken, onCount]);

  /** Перечитать перечень с первой страницы: снятие меняет его целиком. */
  const reload = useCallback(async () => {
    try {
      const page = await loginLane.sessions();
      setRows(page.sessions);
      setNextPageToken(page.nextPageToken);
      setListRefusal(null);
    } catch (e) {
      setListRefusal(laneRefusalOf(e));
    }
  }, []);

  // Первое чтение — подпиской на ответ: исход ложится, только пока раздел жив.
  useEffect(() => {
    let cancelled = false;
    loginLane.sessions().then(
      (page) => {
        if (cancelled) return;
        setRows(page.sessions);
        setNextPageToken(page.nextPageToken);
      },
      (e: unknown) => {
        if (!cancelled) setListRefusal(laneRefusalOf(e));
      },
    );
    return () => {
      cancelled = true;
    };
  }, []);

  const more = async () => {
    if (busy !== null || nextPageToken === "") return;
    setBusy("more");
    try {
      const page = await loginLane.sessions(nextPageToken);
      setRows((prev) => [...(prev ?? []), ...page.sessions]);
      setNextPageToken(page.nextPageToken);
      setListRefusal(null);
    } catch (e) {
      setListRefusal(laneRefusalOf(e));
    }
    setBusy(null);
  };

  /** Шаг снятия: исход строкой либо отказ дословно; перечень перечитывается всегда. */
  const run = async (key: string, step: () => Promise<{ ended: true | number }>) => {
    if (busy !== null) return;
    setBusy(key);
    setRefusal(null);
    try {
      const { ended } = await step();
      announce(outcomeText(ended));
    } catch (e) {
      setRefusal(laneRefusalOf(e));
    }
    await reload();
    setBusy(null);
  };

  const columns: Column<OwnSession>[] = [
    {
      header: "Устройство",
      multiline: true,
      cell: (s) => (
        <>
          <Typography.Text strong style={{ display: "block", whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
            {deviceOf(s)}
          </Typography.Text>
          {s.current && <Tag color="blue">Эта сессия</Tag>}
        </>
      ),
    },
    { header: "Вход", cell: (s) => at(s.authenticatedAt), width: 180 },
    { header: "Последнее предъявление способа входа", cell: (s) => at(s.lastPresentedAt), width: 200 },
    { header: "Действует до", cell: (s) => at(s.expiresAt) },
    {
      header: "",
      cell: (s) =>
        s.current ? null : (
          <Button
            size="small"
            loading={busy === s.id}
            disabled={busy !== null && busy !== s.id}
            aria-label={`Выйти из сессии «${deviceOf(s)}»`}
            onClick={() => void run(s.id, () => loginLane.endSession(holder, s.id))}
          >
            Выйти
          </Button>
        ),
    },
  ];

  const others = rows?.some((s) => !s.current) ?? false;

  return (
    <SecuritySection section="sessions">
      {rows === null && listRefusal === null && <Spin />}
      {listRefusal && (
        <div style={{ marginBottom: 12 }}>
          <LaneRefusalAlert refusal={listRefusal} />
          <Button onClick={() => void reload()} style={{ marginTop: 8 }}>
            Прочитать снова
          </Button>
        </div>
      )}
      {refusal && (
        <div style={{ marginBottom: 12 }}>
          <LaneRefusalAlert refusal={refusal} />
        </div>
      )}
      {rows && (
        <>
          <div style={{ marginBottom: 12 }}>
            <ResourceTable rows={rows} columns={columns} rowKey={(s) => s.id} complete={false} fill={false} />
          </div>
          {!others && nextPageToken === "" && <Typography.Paragraph>{OWN_SESSIONS_TEXT.noOthers}</Typography.Paragraph>}
          {nextPageToken !== "" && (
            <Button
              loading={busy === "more"}
              disabled={busy !== null && busy !== "more"}
              onClick={() => void more()}
              style={{ marginRight: 8 }}
            >
              Показать ещё
            </Button>
          )}
          {(others || nextPageToken !== "") && (
            <Button
              loading={busy === "others"}
              disabled={busy !== null && busy !== "others"}
              onClick={() => setAsking(true)}
            >
              Выйти на всех, кроме этой
            </Button>
          )}
        </>
      )}
      <DangerDialog
        open={asking}
        title="Выйти на всех, кроме этой?"
        consequences={[OWN_SESSIONS_TEXT.endOthersConsequence]}
        confirmText="Выйти"
        onCancel={() => setAsking(false)}
        onConfirm={() => {
          // Диалог закрывается ДО глагола: окно подтверждения, если служба его
          // спросит, встаёт одно.
          setAsking(false);
          void run("others", () => loginLane.endOtherSessions(holder));
        }}
      />
    </SecuritySection>
  );
}
