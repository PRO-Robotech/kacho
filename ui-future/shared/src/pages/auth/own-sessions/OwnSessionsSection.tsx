// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useCallback, useEffect, useId, useState } from "react";
import { Button, Spin, Tag, Typography } from "antd";
import { type LaneRefusal, laneRefusalOf, loginLane, type OwnSession } from "@shared/api/login-lane";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { useFormToken } from "@shared/hooks/use-form-token";
import { formatDateTime } from "@shared/lib/datetime";

// Раздел «Сессии» экрана параметров учётной записи (приёмка F8-S5, Р2–Р5).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО РАЗДЕЛ ДЕЛАЕТ
//
//   • при открытии — ОДНО чтение перечня своих живых сессий; порядок строк —
//     порядок службы, консоль его не переставляет; непустой `nextPageToken` —
//     «Показать ещё», дочитывающее следующую страницу тем же путём;
//   • строка — описание клиента КАК ПРИСЛАНО (Р3: разбора в «браузер» и
//     «систему» нет; ключа нет — «Устройство не названо»; длинное значение
//     переносится, а не обрезается консолью), моменты входа, последнего
//     предъявления способа и срока — в часовом поясе браузера;
//   • текущая — пометкой «Эта сессия» и БЕЗ «Выйти»: текущая завершается
//     выходом, который у консоли уже есть (служба на её снятие отвечает
//     `SESSION_IS_CURRENT`);
//   • «Выйти» у прочих строк и «Выйти на всех, кроме этой» под перечнем —
//     доступна всегда, в том числе при единственной сессии.
//
// После КАЖДОЙ попытки снятия — исход строкой и перечитывание перечня: строка,
// снятая где-то ещё, уходит из перечня тем же ответом, которым служба
// отказала (`SESSION_NOT_FOUND`).
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

function SessionRow({
  session,
  busy,
  disabled,
  onEnd,
}: {
  session: OwnSession;
  busy: boolean;
  disabled: boolean;
  onEnd: () => void;
}) {
  const moments: Array<[string, string | undefined]> = [
    ["Вход", session.authenticatedAt],
    ["Последнее предъявление способа входа", session.lastPresentedAt],
    ["Действует до", session.expiresAt],
  ];
  return (
    <li style={{ display: "flex", alignItems: "flex-start", gap: 12, padding: "8px 0" }}>
      <div style={{ flex: 1, minWidth: 0 }}>
        <Typography.Text strong style={{ display: "block", whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
          {session.userAgent === undefined ? "Устройство не названо" : session.userAgent}
        </Typography.Text>
        {session.current && <Tag color="blue">Эта сессия</Tag>}
        {moments
          .filter(([, at]) => at !== undefined)
          .map(([label, at]) => (
            <Typography.Text key={label} type="secondary" style={{ display: "block" }}>
              {label}: {formatDateTime(at)}
            </Typography.Text>
          ))}
      </div>
      {!session.current && (
        <Button loading={busy} disabled={disabled} onClick={onEnd}>
          Выйти
        </Button>
      )}
    </li>
  );
}

export function OwnSessionsSection() {
  const titleId = useId();
  const holder = useFormToken("session-end");
  const [rows, setRows] = useState<OwnSession[] | null>(null);
  const [nextPageToken, setNextPageToken] = useState("");
  const [listRefusal, setListRefusal] = useState<LaneRefusal | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);

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
    setOutcome(null);
    setRefusal(null);
    try {
      const { ended } = await step();
      setOutcome(outcomeText(ended));
    } catch (e) {
      setRefusal(laneRefusalOf(e));
    }
    await reload();
    setBusy(null);
  };

  return (
    <section aria-labelledby={titleId} style={{ maxWidth: 720, marginBottom: 32 }}>
      <Typography.Title level={4} id={titleId} style={{ marginTop: 0 }}>
        Сессии
      </Typography.Title>
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
      {outcome && (
        <p role="status" style={{ margin: "0 0 12px" }}>
          {outcome}
        </p>
      )}
      {rows && (
        <>
          <ul aria-label="Свои сессии" style={{ listStyle: "none", padding: 0, margin: "0 0 12px" }}>
            {rows.map((s) => (
              <SessionRow
                key={s.id}
                session={s}
                busy={busy === s.id}
                disabled={busy !== null && busy !== s.id}
                onEnd={() => void run(s.id, () => loginLane.endSession(holder, s.id))}
              />
            ))}
          </ul>
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
          <Button
            loading={busy === "others"}
            disabled={busy !== null && busy !== "others"}
            onClick={() => void run("others", () => loginLane.endOtherSessions(holder))}
          >
            Выйти на всех, кроме этой
          </Button>
        </>
      )}
    </section>
  );
}
