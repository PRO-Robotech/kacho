import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const source = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "registerExtensions.tsx"), "utf8");

describe("SubjectPrivilegesTab — subject branch", () => {
  it("queries listSubjectPrivileges (admin-visible) for the subject branch", () => {
    // FIX 1: subject-ветка ходит в listSubjectPrivileges (self OR account-admin),
    // а НЕ в self-only listAccessBindingsBySubject → админ видит привилегии SA.
    expect(source).toContain("iamApi.listSubjectPrivileges(");
  });

  it("stops using the self-only listAccessBindingsBySubject in the subject branch", () => {
    // Резолв листинга по субъекту через self-only RPC приводил к 403 у админа →
    // «Привилегий нет.». Он больше не вызывается из этой вкладки.
    expect(source).not.toContain("iamApi.listAccessBindingsBySubject(");
  });

  it("surfaces the query error instead of a false empty-state", () => {
    // isError-ветка рендерит ApiError-сообщение (ErrorResult), а не «Привилегий нет.».
    expect(source).toContain("ErrorResult");
    expect(source).toMatch(/isError/);
  });

  it("renders server-resolved role_name for subject privileges", () => {
    // SubjectPrivilege несёт role_name (resolved сервером) — subject-ветка рендерит
    // его напрямую, без локального roleNameById-резолва.
    expect(source).toContain("role_name");
  });
});
