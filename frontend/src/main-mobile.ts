import './style.css'
import AppMobile from './AppMobile.svelte'

const app = new AppMobile({
  target: document.getElementById('app')!,
})

export default app
