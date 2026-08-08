import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import type { AuthContextValue } from "@shared/contexts/AuthContext";
import type { RequireMFAFresh as RequireMFAFreshExport } from "./RequireMFAFresh";

const auth = { loading: false, mfaFreshUntil: 0 } as unknown as AuthContextValue;
let fresh = false;

jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({
  useAuth: () => auth,
  isMfaFresh: () => fresh,
}));

// Адрес входа возвращается ФРАГМЕНТОМ (`#…`) намеренно, и это не косметика.
// `window.location` в jsdom — [LegacyUnforgeable]: и сам он, и его `assign`
// объявлены неперенастраиваемыми и незаписываемыми (проверено дескрипторами),
// поэтому подменить или подсмотреть переход подстановкой нельзя. Переход же
// внутри документа (смена фрагмента) jsdom выполняет по-настоящему — значит
// СОБРАННЫЙ компонентом адрес целиком читается из `window.location.hash`.
// Так утверждается то, что компонент действительно построил и куда действительно
// ушёл, а не факт вызова подставного.
jest.unstable_mockModule("@shared/lib/kratos", () => ({
  kratos: { loginUrl: (returnTo?: string) => `#idp-login?return_to=${encodeURIComponent(returnTo ?? "")}` },
}));

let RequireMFAFresh: typeof RequireMFAFreshExport;

const navigatedTo = () => window.location.hash;

const renderAt = (path: string, node: React.ReactNode) =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/admins" element={node} />
      </Routes>
    </MemoryRouter>,
  );

describe("RequireMFAFresh", () => {
  beforeAll(async () => {
    ({ RequireMFAFresh } = await import("./RequireMFAFresh"));
  });

  beforeEach(() => {
    fresh = false;
    auth.loading = false;
    window.location.hash = "";
  });

  it("свежую 2FA пропускает к содержимому и никуда не уводит", () => {
    fresh = true;

    renderAt("/admins", <RequireMFAFresh>{<div data-testid="admins" />}</RequireMFAFresh>);

    expect(screen.getByTestId("admins")).toBeInTheDocument();
    expect(navigatedTo()).toBe("");
  });

  it("пока личность грузится — не уводит и содержимое не показывает", () => {
    auth.loading = true;

    renderAt("/admins", <RequireMFAFresh>{<div data-testid="admins" />}</RequireMFAFresh>);

    expect(screen.queryByTestId("admins")).not.toBeInTheDocument();
    expect(screen.queryByTestId("require-mfa-fresh")).not.toBeInTheDocument();
    expect(navigatedTo()).toBe("");
  });

  it("без свежей 2FA при autoTrigger сам уходит на повторный вход с aal2", () => {
    renderAt("/admins?tab=json", <RequireMFAFresh>{<div data-testid="admins" />}</RequireMFAFresh>);

    expect(navigatedTo()).toBe(`#idp-login?return_to=${encodeURIComponent("/admins?tab=json")}&refresh=true&aal=aal2`);
  });

  it("без autoTrigger сам не уходит, а предлагает подтвердить — и содержимое всё равно закрыто", () => {
    renderAt("/admins", <RequireMFAFresh autoTrigger={false}>{<div data-testid="admins" />}</RequireMFAFresh>);

    const prompt = screen.getByTestId("require-mfa-fresh");
    expect(prompt).toHaveAttribute("title", "Подтвердите свежесть MFA");
    // Отказ обязан быть закрытым: без свежей 2FA содержимое не показывается ни
    // при каком значении autoTrigger — переключатель управляет только тем, уводит
    // ли guard пользователя сам.
    expect(screen.queryByTestId("admins")).not.toBeInTheDocument();
    expect(navigatedTo()).toBe("");
  });
});
