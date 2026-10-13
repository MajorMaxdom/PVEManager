import { mount } from 'svelte'
import './styles/app.css'
import App from './App.svelte'

document.documentElement.lang = navigator.language?.startsWith('de') ? 'de' : 'en'
export default mount(App, { target: document.getElementById('app')! })
