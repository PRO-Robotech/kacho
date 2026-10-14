// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useState } from "react";
import { Alert, Button, Checkbox, Modal } from "antd";

// Окно «Сохраните запасные коды» (приёмка F8-S7, Р4).
//
// Коды показываются ОДИН раз — в ответе подтверждения заведения и перевыпуска:
// служба их больше не отдаёт (SUA-60). Поэтому окно не закрывается ни крестиком,
// ни Esc, ни кликом мимо, а кнопка «Готово» доступна только после отметки «Я
// сохранил коды»: человек, закрывший окно не глядя, остался бы без способа
// войти при утрате приложения. Коды живут в состоянии окна и уходят вместе с
// ним — ни в адрес, ни в хранилище браузера, ни в журнал.

/** Имя файла кодов — без адреса и имени человека: файл может лечь куда угодно. */
const CODES_FILE = "kacho-backup-codes.txt";

function download(codes: readonly string[]) {
  const blob = new Blob([`${codes.join("\n")}\n`], { type: "text/plain;charset=utf-8" });
  const href = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = href;
  a.download = CODES_FILE;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(href);
}

export function SavedCodesDialog({
  codes,
  regenerated,
  onDone,
}: {
  /** Коды из ответа службы; `null` — окна нет. */
  codes: readonly string[] | null;
  /** Коды выпущены взамен прежних. */
  regenerated: boolean;
  onDone: () => void;
}) {
  const [saved, setSaved] = useState(false);
  const [copied, setCopied] = useState<"done" | "failed" | null>(null);

  const copy = async () => {
    if (!codes) return;
    try {
      await navigator.clipboard.writeText(codes.join("\n"));
      setCopied("done");
    } catch {
      setCopied("failed");
    }
  };

  return (
    <Modal
      open={codes !== null}
      title="Сохраните запасные коды"
      closable={false}
      keyboard={false}
      mask={{ closable: false }}
      afterOpenChange={(isOpen) => {
        if (!isOpen) {
          setSaved(false);
          setCopied(null);
        }
      }}
      footer={[
        <Button key="copy" onClick={() => void copy()}>
          Копировать
        </Button>,
        <Button key="download" onClick={() => codes && download(codes)}>
          Скачать (.txt)
        </Button>,
        <Button key="done" type="primary" disabled={!saved} onClick={onDone}>
          Готово
        </Button>,
      ]}
    >
      <Alert
        type="warning"
        showIcon
        message={
          regenerated
            ? "Прежние запасные коды больше недействительны. Новые показываются один раз — сохраните их сейчас."
            : "Запасные коды показываются один раз — сохраните их сейчас. Каждый код срабатывает один раз."
        }
        style={{ marginBottom: 12 }}
      />
      <ul aria-label="Запасные коды" style={{ fontFamily: "monospace", columns: 2, paddingLeft: 20 }}>
        {(codes ?? []).map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
      {copied && (
        <p role="status" style={{ margin: "0 0 12px" }}>
          {copied === "done" ? "Коды скопированы" : "Скопировать не удалось — скачайте коды файлом"}
        </p>
      )}
      <Checkbox checked={saved} onChange={(e) => setSaved(e.target.checked)}>
        Я сохранил коды
      </Checkbox>
    </Modal>
  );
}
