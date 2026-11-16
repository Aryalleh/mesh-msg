// Two browsers (phone-sized) chat through web/cmd/devboard: over the board
// link, then over the relay alone (cross-origin: CORS). scripts/test-web.sh.
const { chromium } = require('playwright');
const SHOTS = process.argv[2];
(async () => {
  const browser = await chromium.launch();
  const mk = async (name, board) => {
    const ctx = await browser.newContext({ viewport: { width: 390, height: 760 }, deviceScaleFactor: 1, locale: 'fa-IR' });
    await ctx.addInitScript(([board]) => {
      localStorage.setItem('mava.relays', 'http://127.0.0.1:8081');
      if (board) localStorage.setItem('mava.board', board);
    }, [board]);
    const p = await ctx.newPage();
    p.on('console', (m) => { if (m.type() === 'error' || m.type() === 'warning') console.log(name, m.type(), m.text()); });
    p.on('pageerror', (e) => console.log(name, 'pageerror', e.message));
    await p.goto('http://127.0.0.1:8080/app/index.htm');
    await p.waitForSelector('#name', { timeout: 30000 });
    await p.fill('#name', name);
    await p.click('#go');
    await p.waitForSelector('#mylink');
    return p;
  };
  const run = async (label, boardA, boardB) => {
    const a = await mk('Ali ' + label, boardA);
    const b = await mk('Sara ' + label, boardB);
    const linkA = await a.textContent('#mylink');
    await b.fill('#uri', linkA);
    await b.fill('#cname', 'علی');
    await b.click('#add');
    await b.fill('#text', 'سلام علی! از آیفون (' + label + ')');
    await b.click('#send');
    await a.waitForSelector('.contact', { timeout: 30000 });
    await a.click('.contact');
    await a.waitForSelector('.msg.theirs', { timeout: 30000 });
    console.log(label, 'Ali got:', await a.textContent('.msg.theirs'));
    await a.fill('#text', 'سلام سارا 👋 ۱۴۰۵');
    await a.click('#send');
    await b.waitForFunction(() => document.querySelectorAll('.msg.theirs').length === 1, null, { timeout: 30000 });
    await b.waitForFunction(() => document.querySelector('.msg.mine small').textContent.includes('✓✓'), null, { timeout: 40000 });
    console.log(label, 'Sara sees delivered + reply:', await b.textContent('.msg.theirs'));
    console.log(label, 'status A:', await a.textContent('#status'), '| B:', await b.textContent('#status'));
    if (SHOTS) {
      await b.screenshot({ path: SHOTS + '/web-chat-' + label + '.png' });
      await a.click('#back');
      await a.screenshot({ path: SHOTS + '/web-chats-' + label + '.png' });
    }
  };
  await run('board', '', '');
  await run('relay', '-', '-');
  await browser.close();
  console.log('PASS');
})().catch((e) => { console.error('FAIL', e); process.exit(1); });
