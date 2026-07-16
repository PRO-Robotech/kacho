import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { Operation } from "@shared/api/types";
// Импортируем только чистый nav-helper (без antd/react-query — иначе jest-граф
// с `antd/es/table` подвешивает импорт runtime-модуля GroupsPage).
import { groupDetailPathFromOp } from "./groupNav";

const source = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "GroupsPage.tsx"), "utf8");

describe("groupDetailPathFromOp", () => {
  it("navigates to the created group detail when metadata.group_id is present", () => {
    const op: Operation = { id: "op-1", done: true, metadata: { "@type": "…", group_id: "grp-abc" } };
    expect(groupDetailPathFromOp(op)).toBe("/iam/groups/grp-abc");
  });

  it("falls back to the groups list when metadata.group_id is absent", () => {
    expect(groupDetailPathFromOp({ id: "op-1", done: true })).toBe("/iam/groups");
    expect(groupDetailPathFromOp({ id: "op-1", done: true, metadata: { "@type": "…" } })).toBe("/iam/groups");
    expect(groupDetailPathFromOp(undefined)).toBe("/iam/groups");
  });

  it("wires the create onSuccess to the new group detail via the nav helper", () => {
    // FIX 4: onSuccess(op) → navigate(groupDetailPathFromOp(op)); больше не
    // безусловный navigate("/iam/groups").
    expect(source).toContain("onSuccess: (op)");
    expect(source).toContain("navigate(groupDetailPathFromOp(op))");
  });
});

describe("GroupCreatePage form", () => {
  it("drops the read-only Account Form.Item and adds a labels editor", () => {
    // FIX 5: read-only «Account» field удалён; форма — name + labels + description.
    expect(source).not.toContain('<Form.Item label="Account">');
    expect(source).toContain("LabelsEditor");
    expect(source).toContain('<Form.Item label="Метки">');
    // account_id всё ещё уходит в тело POST (auto-derived из host-контекста).
    expect(source).toContain("account_id: accountId");
  });
});
