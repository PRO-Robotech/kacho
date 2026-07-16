// Jest-заглушка для CSS-импортов (`import "@/index.css"`): в jsdom реальный CSS
// не парсится, а vite-сборка обрабатывает стили сама. NlbPage импортирует
// index.css/typography.css, чтобы remote нёс свои стили в host — под тестом
// эти импорты резолвятся сюда как пустой модуль (side-effect only).
export default {};
