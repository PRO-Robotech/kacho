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

  // Fetch users when debounced query changes (and modal is open).
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setOptionsLoading(true);
    iamApi
      .listUsers({ pageSize: "20" })
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
        // Client-side filter by email/display_name (UserService.List backend
        // does not support arbitrary `filter` expressions in this phase —
        // fetch top-20 and filter locally).
        const q = debounced.trim().toLowerCase();
        const filtered = q
          ? users.filter(
              (u) =>
                (u.email ?? "").toLowerCase().includes(q) ||
                (u.display_name ?? "").toLowerCase().includes(q) ||
                u.id.toLowerCase().includes(q),
            )
          : users;
        setOptions(
          filtered.map((u) => ({
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
    qc.invalidateQueries({ queryKey: ["cluster-admins"] });
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
      const id = resp.operation?.id;
      if (id) {
        setOpId(id);
      } else {
        // sync success (no operation envelope) — корректность сомнительная, но
        // покрываем по аналогии с другими IAM-мутациями.
        toast.success("Admin granted");
        qc.invalidateQueries({ queryKey: ["cluster-admins"] });
        setSubmitting(false);
        handleClose();
      }
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : e instanceof Error ? e.message : "Ошибка";
      toast.error(msg);
      setSubmitting(false);
    }
  };

  const placeholder = useMemo(() => "Email, имя или usr_… (минимум 2 символа)", []);

  return (
    <Modal
      title={
        <span>
          <UserAddOutlined style={{ marginRight: 8 }} />
          Выдать cluster admin
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
                const opt = option as unknown as UserOption;
                setSelectedUser(opt.user);
                setQuery(opt.user.email || opt.user.id);
              }}
              placeholder={placeholder}
              notFoundContent={optionsLoading ? <Spin size="small" /> : "Нет совпадений"}
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
            Cluster admin получает все права на ресурсы кластера (FGA-relation
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
