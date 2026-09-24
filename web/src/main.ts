import { createApp } from 'vue'
import '@fontsource/fira-sans/cyrillic-400.css'
import '@fontsource/fira-sans/latin-400.css'
import '@fontsource/fira-sans/cyrillic-500.css'
import '@fontsource/fira-sans/latin-500.css'
import '@fontsource/fira-sans/cyrillic-600.css'
import '@fontsource/fira-sans/latin-600.css'
import '@fontsource-variable/tektur/standard.css'
import App from './App.vue'
import { router } from './router'
import './styles/main.css'

createApp(App).use(router).mount('#app')
