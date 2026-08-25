// GrantAdminModal — KAC-196 Task 5.
//
// AntD Modal с AutoComplete над списком User'ов. Debounce 300ms:
//   - input change → выставляет внутренний `query`;
//   - effect: после 300ms тишины — fetch /iam/v1/users → опции AutoComplete;
//   - выбор → store userId → "Выдать" → clusterApi.grantAdmin → poll Operation
//     → toast + close + invalidate ["cluster-admins"] (родитель).
//
// Не закрываемся при ошибке (см. kacho-ui CLAUDE.md §3.5 «Error handling в
// мутирующих формах»).

import { useEffect, useMemo, useState } from "react";
import { AutoComplete, Button, Form, Modal, Spin, Typography } from "antd";
import { UserAddOutlined } from "@ant-design/icons";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";
import { clusterApi } from "@shared/api/cluster";
import { iamApi, type User } from "@shared/api/iam";
import { useOperation } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";
import { resolveMutationResponse } from "@shared/lib/operation-outcome";
import { pickerScope } from "@shared/lib/picker-search";

/**
 * Чем сужается список пользователей у владельца: выделенным словом `search`,
 * а не подстрокой по полю. iam отвергает `CONTAINS` явно (`InvalidArgument` на
 * всю страницу), поэтому подставить сюда общий механизм списков нельзя — и
 * ровно ради этого различия у области поиска два ключа, а не один.
 */
const USERS_SCOPE = pickerScope({ serverTerm: "search" });

interface Props {
  open: boolean;
  onClose: () => void;
}

interface UserOption {
  value: string; // user.id
  label: React.ReactNode;
  user: User;
}

export function GrantAdminModal({ open, onClose }: Props) {
  const qc = useQueryClient();
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  const [options, setOptions] = useState<UserOption[]>([]);
  const [optionsLoading, setOptionsLoading] = useState(false);
  const [selectedUser, setSelectedUser] = useState<User | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [opId, setOpId] = useState<string | null>(null);

  // 300ms debounce — input → debounced query.
  useEffect(() => {
    const t = setTimeout(() => setDebounced(query), 300);
    return () => clearTimeout(t);
  }, [query]);

  // Ввод спрашивает СЕРВЕР (#528).
  //
  // Прежде здесь читались первые двадцать пользователей и сужались в браузере —
  // с честным комментарием, что владелец фильтровать не умеет. Комментарий
  // пережил свой предмет: `ListUsersRequest.filter` принимает выделенное слово
  // `search="…"` — подстроку по почте И идентификатору сразу (у пользователя
  // имени нет вовсе, его узнают по почте, поэтому владелец и завёл отдельное
  // слово вместо `name CONTAINS`).
  //
  // Цена прежней формы измеряется одним числом: двадцать. Двадцать первый
  // администратор был недостижим НИКАКИМ вводом, а поле отвечало «нет
  // совпадений» — то есть утверждало об отсутствии человека то, чего не
  // спрашивало. Задержка ввода при этом уже стояла и тратила круг до края,
  // перечитывая те же двадцать строк.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setOptionsLoading(true);
    iamApi
      .listUsers({ pageSize: "20", ...USERS_SCOPE.query(debounced) })
      .then((data) => {
        if (cancelled) return;
        // KAC-196 follow-up: filter out PENDING (invited but not registered via
        // Kratos yet) and BLOCKED users. Granting cluster-admin к PENDING-user'у
        // бессмысленно — у него нет Kratos identity, он физически не сможет
        // авторизоваться. KAC-125 multi-account users могут иметь дубликаты email
        // (один человек invited в N accounts) — каждый row имеет unique user.id,
        // но email duplicates захламляют AutoComplete. ACTIVE-only фильтр чистит
        // PENDING/BLOCKED; затем dedup-by-email оставляет один row per email
        // (cluster-admin — singleton scope, account_id не важен).
        const active = (data?.users ?? []).filter((u) => !u.invite_status || u.invite_status === "ACTIVE");
        const seenEmails = new Set<string>();
        const users: typeof active = [];
        for (const u of active) {
          const key = (u.email ?? u.id).toLowerCase();
          if (seenEmails.has(key)) continue;
          seenEmails.add(key);
          users.push(u);
        }
        // Сузил сервер — в браузере не пересеиваем: `search` смотрит на почту и
        // идентификатор, а показанное имя приходит из профиля и с ними может не
        // совпасть. Повторное сужение вычло бы из ответа края строки, которые он
        // прислал именно по этому вводу.
        setOptions(
          users.map((u) => ({
            value: u.id,
            label: (
              <span>
                <Typography.Text strong>{u.email || u.id}</Typography.Text>
                {u.display_name && (
                  <Typography.Text type="secondary" style={{ marginLeft: 6 }}>
                    · {u.display_name}
                  </Typography.Text>
                )}
              </span>
            ),
            user: u,
          })),
        );
      })
      .catch(() => {
        if (!cancelled) setOptions([]);
      })
      .finally(() => {
        if (!cancelled) setOptionsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [debounced, open]);

  // Poll операцию до done; success → toast, invalidate, close.
  const { data: op } = useOperation(opId);
  useEffect(() => {
    if (!op?.done || !opId) return;
    if (op.error) {
      toast.error(op.error.message || "Не удалось выдать admin");
      setSubmitting(false);
      setOpId(null);
      return;
    }
    toast.success("Admin granted");
    void qc.invalidateQueries({ queryKey: ["cluster-admins"] });
    setSubmitting(false);
    setOpId(null);
    handleClose();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [op?.done, op?.error, opId]);

  const handleClose = () => {
    setQuery("");
    setDebounced("");
    setOptions([]);
    setSelectedUser(null);
    setSubmitting(false);
    setOpId(null);
    onClose();
  };

  const handleSubmit = async () => {
    if (!selectedUser) return;
    setSubmitting(true);
    try {
      const resp = await clusterApi.grantAdmin(selectedUser.id);
      // Исход читается общим разбором: край отдаёт Operation ВЕРХНИМ уровнем,
      // поэтому вложенный ключ был пуст всегда и ветка «нет операции» работала
      // на каждой выдаче. Здесь стоял и честный комментарий автора про
      // «корректность сомнительная» — сомнение было обоснованным.
      //
      // `GrantAdmin` объявлен `returns (operation.Operation)`, поэтому ответ без
      // операции — нарушение контракта, а не синхронный успех.
      const resolved = resolveMutationResponse(resp, true);
      if (resolved.kind === "operation") {
        setOpId(resolved.opId);
      } else {
        toast.error(resolved.kind === "violation" ? resolved.message : "Ответ без операции");
        setSubmitting(false);
      }
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : e instanceof Error ? e.message : "Ошибка";
      toast.error(msg);
      setSubmitting(false);
    }
  };

  // Подпись называет то, чем ИЩЕТСЯ, а не то, что показано. Прежняя обещала
  // сужение по имени (владелец по нему не ищет: у пользователя имени нет) и
  // порог в два символа, которого никто не проверял, — два обещания, за
  // которыми ничего не стояло.
  const placeholder = useMemo(() => "Почта или usr_… — ищется по всему списку", []);

  return (
    <Modal
      title={
        <span>
          <UserAddOutlined style={{ marginRight: 8 }} />
          Выдать права администратора кластера
        </span>
      }
      open={open}
      onCancel={handleClose}
      maskClosable={!submitting}
      destroyOnHidden
      width={600}
      // AntD прокидывает rootClassName на корневую обёртку (которая может быть
      // hidden даже когда модалка открыта — display:none на ant-modal-root).
      // Сам контент-блок (ниже через data-testid="grant-admin-modal-body")
      // mount'ится только при `open=true`, его и проверяем в e2e.
      footer={[
        <Button key="cancel" onClick={handleClose} disabled={submitting}>
          Отмена
        </Button>,
        <Button
          key="ok"
          type="primary"
          loading={submitting}
          disabled={!selectedUser}
          onClick={handleSubmit}
          data-testid="grant-admin-submit"
        >
          Выдать
        </Button>,
      ]}
    >
      <div data-testid="grant-admin-modal-body">
        <Form
          layout="horizontal"
          labelCol={{ flex: "160px" }}
          wrapperCol={{ flex: "auto" }}
          labelAlign="left"
          colon={false}
        >
          <Form.Item label="Пользователь" required>
            <AutoComplete
              options={options}
              value={query}
              onSearch={setQuery}
              onChange={(v) => {
                setQuery(v);
                if (!v) setSelectedUser(null);
              }}
              onSelect={(_value, option) => {
                const opt = option;
                setSelectedUser(opt.user);
                setQuery(opt.user.email || opt.user.id);
              }}
              placeholder={placeholder}
              title={USERS_SCOPE.notice}
              // Пустой ответ называет свою ОБЛАСТЬ. «Нет совпадений» здесь
              // означало «нет среди двадцати прочитанных» — и читалось как
              // «такого человека нет».
              notFoundContent={optionsLoading ? <Spin size="small" /> : USERS_SCOPE.emptyText}
              data-testid="grant-admin-search"
              style={{ width: "100%" }}
            />
          </Form.Item>
          {selectedUser && (
            <Form.Item label="ID">
              <Typography.Text code>{selectedUser.id}</Typography.Text>
            </Form.Item>
          )}
          <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0, marginLeft: 160 }}>
            Администратор облака получает все права на ресурсы кластера (роль
            <Typography.Text code style={{ fontSize: 12 }}>
              system_admin
            </Typography.Text>
            ). Действие необратимо до явного отзыва.
          </Typography.Paragraph>
        </Form>
      </div>
    </Modal>
  );
}
