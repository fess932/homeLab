// Переход через chrome.tabs, а не location: так курсор остаётся в адресной строке, как на стандартной новой вкладке.
const url = localStorage.getItem('url')
if (!url) location.replace('options.html')
else chrome.tabs.getCurrent((tab) => chrome.tabs.update(tab.id, { url }))
