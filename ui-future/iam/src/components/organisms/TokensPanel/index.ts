export { TokensPanel } from "./TokensPanel";
// `IssuedSecret` снят вместе со своим предметом: одноразовое значение приходит
// в ДВУХ формах (секрет либо ключевая пара), и общая структура с двумя
// необязательными полями заставляла каждого читателя решать заново, что перед
// ним. Размеченный вид живёт в `@shared/api/tokens` (`IssuedCredential`).
export type { IssueTokenBody, TokenRow, TokensPanelProps } from "./TokensPanel";
