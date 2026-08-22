export { AuthCallback } from "./AuthCallback";
// Прямо у общей реализации, без прослойки в модуле: та была ре-экспортом
// `@shared/pages/auth/Login` и своего не несла, а звал её один этот барель —
// то есть модуль, не достижимый ни от одного входа приложения. Снята вместе
// со сведением форка; поверхность бареля не изменилась.
export { AuthLayout, bufferToBase64Url, LoginPage } from "@shared/pages/auth/Login";
export { LogoutPage } from "./Logout";
export { RecoveryPage } from "./Recovery";
export { RegisterPage } from "./Register";
export { SettingsPage } from "./Settings";
export { SignupPage } from "./SignupPage";
