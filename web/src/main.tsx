import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '@fontsource/inter/400.css'
import '@fontsource/inter/500.css'
import '@fontsource/inter/600.css'
import '@fontsource/inter/700.css'
import '@fontsource/fira-code/400.css'
import '@fontsource/fira-code/500.css'
// 中文 UI 子集字体,由 npm run font:subset 生成
import './assets/fonts/noto-sans-sc.css'
import './index.css'
import './i18n'
import { initTheme } from './lib/theme'
import App from './App.tsx'

// 首帧前应用存储的主题（跟随系统偏好），避免闪错主题
initTheme()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
