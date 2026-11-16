// Every translatable node carries its English text in data-en; the Chinese is
// the markup's own text, captured once on load.
const i18n = [...document.querySelectorAll('[data-en]')];
for (const el of i18n) el.dataset.zh = el.textContent.trim();

const toggles = [...document.querySelectorAll('[data-lang]')];

function setLang(lang) {
  document.documentElement.lang = lang === 'zh' ? 'zh-CN' : 'en';
  for (const el of i18n) el.textContent = el.dataset[lang];
  for (const b of toggles) b.setAttribute('aria-pressed', String(b.dataset.lang === lang));
}

for (const b of toggles) b.addEventListener('click', () => setLang(b.dataset.lang));

// The screenshot lives in docs/ in the repository. When only this folder is
// published, fall back to the copy on GitHub. The check after addEventListener
// catches an error that fired before this script ran.
const shot = document.querySelector('.shot img');
if (shot) {
  const fallback = 'https://raw.githubusercontent.com/xiyaowong/outrash/main/docs/screenshot.png';
  const retry = () => {
    if (shot.src !== fallback) shot.src = fallback;
  };
  shot.addEventListener('error', retry);
  if (shot.complete && shot.naturalWidth === 0) retry();
}
