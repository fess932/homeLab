const form = document.getElementById('form')
const input = document.getElementById('url')
const saved = document.getElementById('saved')

input.value = localStorage.getItem('url') ?? ''

form.addEventListener('submit', (event) => {
  event.preventDefault()
  const url = new URL(input.value)
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    input.setCustomValidity('Нужен адрес http или https')
    input.reportValidity()
    return
  }
  localStorage.setItem('url', url.href)
  saved.hidden = false
})

input.addEventListener('input', () => {
  input.setCustomValidity('')
  saved.hidden = true
})
