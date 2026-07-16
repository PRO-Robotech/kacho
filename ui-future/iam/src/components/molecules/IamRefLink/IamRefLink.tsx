// IamRefLink живёт в shared (app-agnostic), чтобы REGISTRY-колонки IAM-ресурсов
// резолвились в любом app'е. Здесь — тонкий re-export для локальных импортов
// `@/components/molecules/IamRefLink`.
export { IamRefLink } from "@shared/components/molecules/IamRefLink";
