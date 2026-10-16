// Run after smoke.mjs, against the same fresh disposable database and real embedded binary.
// Uses no intercepted browser/API responses. PostgreSQL fixtures are removed in finally.
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { execFileSync, spawn } from "node:child_process";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { chromium, request } from "playwright";
import { en as aEn, ko as aKo } from "../web/src/i18n/messages/app.ts";
import { en as iEn, ko as iKo } from "../web/src/i18n/messages/interface.ts";
import { en as sEn, ko as sKo } from "../web/src/i18n/messages/settings.ts";

assert.ok(process.env.ARTEX_PG_DSN, "ARTEX_PG_DSN is required; run smoke first against a fresh database");
const dsn = new URL(process.env.ARTEX_PG_DSN);
assert.ok(["postgres:", "postgresql:"].includes(dsn.protocol), "requires a PostgreSQL DSN");
assert.match(dsn.pathname.slice(1), /^scopeweaver_test_/, "requires a disposable database");
assert.ok(process.env.SCOPEWEAVER_BINARY, "SCOPEWEAVER_BINARY is required for completed-task coverage");
const binary = path.resolve(process.env.SCOPEWEAVER_BINARY);
fs.accessSync(binary, fs.constants.R_OK | fs.constants.X_OK);
const out = path.resolve(process.env.E2E_OUT || "e2e-results", "dashboard-token-chart");
fs.mkdirSync(out, { recursive: true });
const prefix = `pr16-dashboard-${randomUUID().replaceAll("-", "")}`;
const texts = {
  en: { old: "Legacy", new: "New", all: "All", miss: "Input (cache miss)", output: "Output", cache: "Cache read", reasoning: aEn.reasoningInOutput, judgeTab: iEn.m1240, overviewTab: iEn.m0147, judgeOutput: sEn.judgeUsageOutput },
  ko: { old: "기존", new: "신규", all: "전체", miss: "입력(캐시 미적중)", output: "출력", cache: "캐시 읽기", reasoning: aKo.reasoningInOutput, judgeTab: iKo.m1240, overviewTab: iKo.m0147, judgeOutput: sKo.judgeUsageOutput },
};
function sql(query) {
  fs.appendFileSync(`${out}/sql.txt`, `$ psql <disposable-dsn> -v ON_ERROR_STOP=1 -tAc ${query}\n`);
  const result = execFileSync("psql", [dsn.href, "-X", "-v", "ON_ERROR_STOP=1", "-qtAc", query], { encoding: "utf8" }).trim();
  fs.appendFileSync(`${out}/sql.txt`, `${result}\n`);
  return result;
}
const today = new Date();
function day(offset) { const d = new Date(today); d.setUTCDate(d.getUTCDate() + offset); return d.toISOString().slice(0, 10); }
// reasoning is a subset of output (llm_usage.reasoning_tokens); it must never be added to output again.
const fixtures = [
  { name: "author-example", profile: "a", date: day(0), input: 332967061, cache: 325608898, output: 1033245, reasoning: 400000 },
  { name: "same-day-b1", profile: "b", date: day(0), input: 10000000, cache: 2000000, output: 3000000, reasoning: 1000000 },
  { name: "same-day-b2", profile: "b", date: day(0), input: 20000000, cache: 6000000, output: 2000000, reasoning: 0 },
  { name: "cache-zero", profile: "a", date: day(-1), input: 9000000, cache: 0, output: 1000000, reasoning: 250000 },
  { name: "all-cached", profile: "a", date: day(-2), input: 7000000, cache: 7000000, output: 1000000, reasoning: 0 },
];
const profiles = {};
const ownedConversations = [];
const ownedUsage = [];
let ownedTask;
let runtime;
let child;
let childDone;
let childResult;
let base;
let browser;
const api = await request.newContext();
const failures = [];
const errors = [];
const cleanupErrors = [];
let bodyError;
async function startServer() {
  runtime = fs.mkdtempSync(path.join(os.tmpdir(), "scopeweaver-dashboard-"));
  const isolatedBinary = path.join(runtime, "scopeweaver");
  // BaseDir also anchors self-update files, so isolate the executable itself.
  fs.copyFileSync(binary, isolatedBinary);
  fs.chmodSync(isolatedBinary, 0o700);
  const skills = path.join(runtime, "skills");
  fs.mkdirSync(skills);
  const config = path.join(runtime, "config.json");
  fs.writeFileSync(config, JSON.stringify({ database: { dsn: dsn.href }, skill_dir: skills }));
  const listener = net.createServer();
  await new Promise((resolve, reject) => { listener.once("error", reject); listener.listen(0, "127.0.0.1", resolve); });
  const port = listener.address().port;
  await new Promise((resolve, reject) => listener.close(e => e ? reject(e) : resolve()));
  base = `http://127.0.0.1:${port}`;
  const log = fs.openSync(path.join(out, "server.log"), "w");
  try {
    child = spawn(isolatedBinary, ["-addr", `127.0.0.1:${port}`, "-data", path.join(runtime, "data"), "-proxy", ""], {
      cwd: runtime,
      // Do not inherit provider credentials, config overrides or skill paths.
      env: { PATH: process.env.PATH, ARTEX_PG_DSN: dsn.href, ARTEX_CONFIG: config, ARTEX_SKILL_DIR: skills },
      stdio: ["ignore", log, log],
    });
    childDone = new Promise(resolve => {
      child.once("error", error => {
        if (!child.pid) { childResult = { error: error.message }; resolve(childResult); }
        else cleanupErrors.push(error); // A failed kill is not proof of process exit.
      });
      child.once("exit", (code, signal) => { childResult = { code, signal }; resolve(childResult); });
    });
  } finally { fs.closeSync(log); }
  for (let i = 0; i < 60; i++) {
    assert.equal(childResult, undefined, `owned server exited: ${JSON.stringify(childResult)}; see ${out}/server.log`);
    try {
      const response = await api.get(`${base}/api/auth/status`, { timeout: 1000 });
      if (response.status() === 200) return;
    } catch { /* The loopback listener may not have bound yet. */ }
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
  throw new Error(`owned server was not ready within 60 attempts; see ${out}/server.log`);
}
async function stopServer() {
  if (!child || childResult) return;
  async function exitedWithin(ms) {
    let timer;
    try { return await Promise.race([childDone, new Promise(resolve => { timer = setTimeout(() => resolve(null), ms); })]); }
    finally { clearTimeout(timer); }
  }
  child.kill("SIGTERM");
  if (await exitedWithin(10000)) return;
  child.kill("SIGKILL");
  assert.ok(await exitedWithin(5000), "owned server did not exit; preserving fixtures and runtime");
}
async function cleanup(label, action) {
  try { await action(); } catch (error) { cleanupErrors.push(new Error(`${label}: ${error.message}`)); }
}
async function get(path, token) {
  const response = await api.get(`${base}/api/${path}`, { headers: { Authorization: `Bearer ${token}` } });
  assert.equal(response.status(), 200, `${path} API status`);
  return response.json();
}
function buckets(rows) {
  const result = new Map();
  for (const r of rows) {
    if (r.date < day(-30)) continue;
    const key = r.date.slice(5);
    const b = result.get(key) || { inputCacheMiss: 0, output: 0, cacheRead: 0, reasoning: 0 };
    b.inputCacheMiss += r.input - r.cache; b.output += r.output; b.cacheRead += r.cache; b.reasoning += r.reasoning;
    result.set(key, b);
  }
  return [...result].sort(([a], [b]) => a.localeCompare(b)).map(([date, b]) => ({ date, ...b }));
}
try {
  assert.equal(sql(`SELECT count(*) FROM llm_profiles WHERE name LIKE '${prefix}-%'`), "0", "fixture names must be unused");
  assert.equal(sql(`SELECT count(*) FROM llm_usage WHERE profile_name LIKE '${prefix}-%'`), "0", "usage fixture names must be unused");
  assert.equal(sql(`SELECT count(*) FROM tasks WHERE status NOT IN ('done','failed','timeout') OR queued`), "0", "fresh smoke database must have no tasks that could run on startup");
  for (const p of ["a", "b"]) profiles[p] = Number(sql(`INSERT INTO llm_profiles(name,format,model,base_url) VALUES ('${prefix}-${p}','openai','qa-only','http://127.0.0.1:1') RETURNING id` ).split("\n")[0]);
  for (const f of fixtures) {
    const timestamp = `${f.date}T12:00:00Z`;
    const conv = Number(sql(`INSERT INTO conversations(agent_key,title,llm_profile_id,created_at) VALUES ('qa-only','${prefix}-${f.name}',${profiles[f.profile]},'${timestamp}') RETURNING id`).split("\n")[0]); ownedConversations.push(conv);
    sql(`INSERT INTO conversation_activities(conversation_id,kind,input_tokens,output_tokens,cache_read_tokens,created_at) VALUES (${conv},'result',${f.input},${f.output},${f.cache},'${timestamp}')`);
    ownedUsage.push(Number(sql(`INSERT INTO llm_usage(ts,worker,profile_name,input_tokens,output_tokens,cache_read,reasoning_tokens,status) VALUES ('${timestamp}','qa-only','${prefix}-${f.profile}',${f.input},${f.output},${f.cache},${f.reasoning},'ok') RETURNING id`)));
  }
  // The task registry is loaded only at startup; use our own server, not the caller's.
  const taskDate = `${day(-3)}T12:00:00Z`;
  const taskIDs = sql(`WITH e AS (INSERT INTO explorations(description,goal,status) VALUES ('${prefix}-task','QA only','achieved') RETURNING id), t AS (INSERT INTO tasks(name,description,goal,exploration_id,llm_profile_id,status,created_at,completed_at) SELECT '${prefix}-task','QA only','QA only',id,${profiles.a},'done','${taskDate}','${taskDate}' FROM e RETURNING id,exploration_id) SELECT id,exploration_id FROM t`).split("|").map(Number);
  ownedTask = { id: taskIDs[0], exploration: taskIDs[1] };
  sql(`INSERT INTO activity(exploration_id,kind,input_tokens,output_tokens,cache_read_tokens,created_at) VALUES (${ownedTask.exploration},'result',12000000,2000000,8000000,'${taskDate}')`);
  ownedUsage.push(Number(sql(`INSERT INTO llm_usage(ts,task_id,exploration_id,worker,model,profile_name,input_tokens,output_tokens,cache_read,reasoning_tokens,status) VALUES ('${taskDate}','${ownedTask.id}',${ownedTask.exploration},'qa-only','${prefix}-model','${prefix}-a',12000000,2000000,8000000,500000,'ok') RETURNING id`)));
  // A fallback-approval (judge) call with reasoning feeds the judge usage card; its profile matches no fixture profile.
  ownedUsage.push(Number(sql(`INSERT INTO llm_usage(ts,worker,profile_name,input_tokens,output_tokens,cache_read,reasoning_tokens,status) VALUES ('${day(-4)}T12:00:00Z','judge','${prefix}-judge',3000000,600000,0,200000,'ok') RETURNING id`)));
  assert.equal(sql(`SELECT count(*) FROM llm_usage WHERE profile_name LIKE '${prefix}-%'`), "7", "seed read-back");
  await startServer();
  const login = await api.post(`${base}/api/auth/login`, { data: { username: "ARTEX", password: "e2e-pass-1234" } });
  assert.equal(login.status(), 200, "run smoke first to initialize password");
  const { token } = await login.json();
  const usage = await get("tokens/usage?days=365", token);
  const tasksResponse = await get("tasks", token);
  const conversations = await get("tokens/conversations", token);
  fs.writeFileSync(`${out}/api-readback.json`, JSON.stringify({ usage, tasks: tasksResponse, conversations }, null, 2));
  const task = tasksResponse.tasks.find(t => t.id === String(ownedTask.id));
  assert.ok(task, "completed task was loaded by the owned server");
  assert.equal(task.status, "done", "fixture must never start an engine run");
  assert.equal(task.llm_profile_id, profiles.a, "completed task profile read-back");
  assert.equal(task.created_at.slice(0, 10), day(-3), "Legacy task created-at day");
  assert.deepEqual(task.tokens, { input_tokens: 12000000, output_tokens: 2000000, cache_read_tokens: 8000000, cache_write_tokens: 0 }, "completed task token read-back");
  const legacy = [
    ...tasksResponse.tasks.map(t => ({ date: t.created_at.slice(0, 10), profile: t.llm_profile_id, input: t.tokens.input_tokens, output: t.tokens.output_tokens, cache: t.tokens.cache_read_tokens })),
    ...conversations.map(c => ({ date: c.created_at.slice(0, 10), profile: c.llm_profile_id, input: c.input_tokens, output: c.output_tokens, cache: c.cache_read_tokens })),
  ];
  const legacyRows = legacy.map(r => ({ ...r, reasoning: 0 }));
  const modern = usage.daily.map(d => ({ date: d.date, profile: d.profile_name, input: d.input_tokens, output: d.output_tokens, cache: d.cache_read_tokens, reasoning: d.reasoning_tokens }));
  assert.deepEqual(usage.daily.find(d => d.profile_name === `${prefix}-a` && d.date === day(0)), { profile_name: `${prefix}-a`, date: day(0), input_tokens: 332967061, output_tokens: 1033245, cache_read_tokens: 325608898, reasoning_tokens: 400000 });
  assert.equal(usage.daily.find(d => d.profile_name === `${prefix}-b` && d.date === day(0)).input_tokens, 30000000, "same-day row aggregate read-back");
  assert.equal(usage.daily.find(d => d.profile_name === `${prefix}-b` && d.date === day(0)).reasoning_tokens, 1000000, "same-day reasoning aggregate read-back");
  assert.deepEqual(usage.by_profile.find(p => p.profile_name === `${prefix}-a`), { profile_name: `${prefix}-a`, calls: 4, tasks: 1, input_tokens: 360967061, output_tokens: 5033245, cache_read_tokens: 340608898, cache_write_tokens: 0, reasoning_tokens: 1150000 }, "profile aggregate keeps reasoning separate from output");
  const byModel = await get(`llm/records/by-model?task=${ownedTask.id}`, token);
  assert.deepEqual(byModel.models, [{ model: `${prefix}-model`, calls: 1, input_tokens: 12000000, output_tokens: 2000000, cache_read_tokens: 8000000, cache_write_tokens: 0, reasoning_tokens: 500000 }], "task model aggregate read-back");
  const judge = await get("intercept/judge/usage?days=30", token);
  assert.ok(judge.reasoning_tokens >= 200000 && judge.output_tokens >= 600000, `judge usage read-back ${JSON.stringify(judge)}`);
  fs.writeFileSync(`${out}/api-usage-readback.json`, JSON.stringify({ byModel, judge }, null, 2));
  browser = await chromium.launch();
  for (const locale of ["en", "ko"]) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    await context.addInitScript(([l, t]) => { localStorage.setItem("scopeweaver.locale", l); localStorage.setItem("artex_token", t); }, [locale, token]);
    const page = await context.newPage(); page.on("pageerror", e => errors.push(`${locale}: ${e.message}`));
    await page.goto(`${base}/dashboard`);
    await page.getByRole("button", { name: `${prefix}-a`, exact: true }).waitFor();
    const panel = page.locator('[data-slot="card"]').filter({ has: page.getByRole("button", { name: texts[locale].new, exact: true }) });
    for (const version of ["old", "new"]) for (const profile of ["all", "a", "b"]) {
      const id = `${locale}-${version}-${profile}`;
      try {
        await panel.getByRole("button", { name: texts[locale][version], exact: true }).click();
        await panel.getByRole("button", { name: profile === "all" ? texts[locale].all : `${prefix}-${profile}`, exact: true }).click();
        const expected = buckets((version === "old" ? legacyRows : modern).filter(r => profile === "all" || r.profile === (version === "old" ? profiles[profile] : `${prefix}-${profile}`)));
        const chart = panel.locator('[data-slot="chart"]'); await chart.locator("path.recharts-rectangle").first().waitFor();
        // Verify actual SVG geometry against API values; the axis is the chart's rendered scale.
        await page.waitForTimeout(1700);
        const rendered = await chart.evaluate(el => {
          const tick = t => ({ text: t.textContent, x: Number(t.getAttribute("x")), y: Number(t.getAttribute("y")) });
          return { xTicks: [...el.querySelectorAll(".recharts-xAxis-tick-labels text")].map(tick),
            yTicks: [...el.querySelectorAll(".recharts-yAxis-tick-labels text")].map(tick),
            groups: [...el.querySelectorAll(".recharts-bar")].map(group => [...group.querySelectorAll("path.recharts-rectangle")].map(p => {
              const box = p.getBBox(); return { x: Number(p.getAttribute("x")), width: box.width, height: box.height,
                fill: getComputedStyle(p).fill, css: getComputedStyle(el).getPropertyValue("--color-inputCacheMiss"), d: p.getAttribute("d") };
            })) };
        });
        fs.writeFileSync(`${out}/${id}-dom.json`, JSON.stringify({ expected, rendered }, null, 2));
        assert.equal(rendered.groups.length, 3, "three stack series");
        const fmt = n => n >= 1000000 ? (n / 1000000).toFixed(1) + "M" : n >= 1000 ? (n / 1000).toFixed(1) + "k" : String(n);
        const parseToken = text => Number(text.replace(/[kM]/g, "")) * (text.endsWith("M") ? 1000000 : text.endsWith("k") ? 1000 : 1);
        const [y0, y1] = rendered.yTicks;
        const tokensPerPixel = (parseToken(y1.text) - parseToken(y0.text)) / (y0.y - y1.y);
        const tolerance = Math.max(1, tokensPerPixel * 0.001);
        for (const row of [...expected].sort((a, b) => b.cacheRead - a.cacheRead)) {
          const tick = rendered.xTicks.find(t => t.text === row.date); assert.ok(tick, `${id} ${row.date} actual axis tick`);
          const segments = rendered.groups.map(g => g.find(s => Math.abs(s.x + s.width / 2 - tick.x) < 1));
          const stackValues = segments.map(s => (s?.height || 0) * tokensPerPixel);
          const expectedValues = [row.inputCacheMiss, row.output, row.cacheRead];
          const actualTotal = stackValues.reduce((a,b)=>a+b,0), expectedTotal = expectedValues.reduce((a,b)=>a+b,0);
          assert.ok(Math.abs(actualTotal - expectedTotal) <= tolerance, `${id} ${row.date} rendered stack total ${actualTotal} must equal cache-inclusive input + output ${expectedTotal} (tolerance ${tolerance})`);
          for (let j = 0; j < 3; j++) {
            assert.ok(Math.abs(stackValues[j] - expectedValues[j]) <= tolerance, `${id} ${row.date} segment ${j} ${stackValues[j]} expected ${expectedValues[j]}`);
            if (expectedValues[j] > 0) assert.ok(segments[j].width > 0 && segments[j].height > 0, `${id} ${row.date} segment ${j} positive SVG size`);
          }
          if (row.inputCacheMiss > 0) assert.equal(segments[0].fill, "rgb(60, 131, 246)", `${id} resolved blue fill`);
        }
        assert.ok((await panel.textContent()).includes(texts[locale].miss), `${id} cache-miss legend`);
        if (version === "new") {
          const reasoning = usage.by_profile.filter(p => profile === "all" || p.profile_name === `${prefix}-${profile}`).reduce((a, p) => a + p.reasoning_tokens, 0);
          // A hidden tooltip from an earlier hover can stay in the DOM; read only the visible metric.
          const line = (await panel.getByText(texts[locale].reasoning, { exact: true }).filter({ visible: true }).locator("xpath=..").textContent()).trim();
          assert.equal(line, `${texts[locale].reasoning}${fmt(reasoning)}`, `${id} reasoning metric`);
        } else assert.equal(await panel.getByText(texts[locale].reasoning, { exact: true }).filter({ visible: true }).count(), 0, `${id} legacy view shows reasoning`);
        // Real pointer hover exercises the actual visible tooltip on every day.
        await chart.scrollIntoViewIfNeeded();
        const svgBox = await chart.locator("svg.recharts-surface").boundingBox();
        const tooltips = [];
        for (const row of [...expected].sort((a, b) => b.cacheRead - a.cacheRead)) {
          const tick = rendered.xTicks.find(t => t.text === row.date);
          await page.mouse.move(svgBox.x + tick.x, svgBox.y + y0.y - 2);
          const tooltip = chart.locator(".recharts-tooltip-wrapper"); await tooltip.waitFor({ state: "visible" });
          const text = await tooltip.textContent();
          for (const label of [texts[locale].miss, texts[locale].output, texts[locale].cache]) assert.ok(text.includes(label), `${id} ${row.date} actual tooltip label ${label}`);
          for (const value of [row.inputCacheMiss, row.output, row.cacheRead]) assert.ok(text.includes(fmt(value)), `${id} ${row.date} actual tooltip value ${fmt(value)}`);
          // Reasoning is shown as part of output, never as a fourth stacked series.
          if (row.reasoning > 0) assert.ok(text.includes(`${texts[locale].reasoning}${fmt(row.reasoning)}`), `${id} ${row.date} tooltip reasoning ${fmt(row.reasoning)} in ${text}`);
          else assert.ok(!text.includes(texts[locale].reasoning), `${id} ${row.date} tooltip shows zero reasoning`);
          tooltips.push({ date: row.date, text });
        }
        fs.writeFileSync(`${out}/${id}-tooltips.json`, JSON.stringify(tooltips, null, 2));
        await page.screenshot({ path: `${out}/${id}.png`, fullPage: true });
        console.log(`PASS ${id}: ${JSON.stringify(expected)}`);
      } catch (e) { failures.push(id); console.log(`FAIL ${id}: ${e.message}`); await page.screenshot({ path: `${out}/${id}-failed.png`, fullPage: true }); }
    }
    // Task overview model table and the judge usage card show reasoning beside, not inside, output.
    const scenario = `${locale}-task-judge`;
    try {
      await page.goto(`${base}/function/tasks/detail?id=${ownedTask.id}`);
      await page.getByRole("tab", { name: texts[locale].overviewTab, exact: true }).click();
      const row = page.locator("tr").filter({ has: page.getByText(`${prefix}-model`, { exact: true }) });
      await row.waitFor();
      const headers = (await row.locator("xpath=ancestor::table").locator("th").allTextContents()).map(h => h.trim());
      const cells = (await row.locator("td").allTextContents()).map(c => c.trim());
      fs.writeFileSync(`${out}/${scenario}-task.json`, JSON.stringify({ headers, cells }, null, 2));
      assert.equal(headers[3], texts[locale].output, `${scenario} output header`);
      assert.equal(headers[4], texts[locale].reasoning, `${scenario} reasoning header`);
      assert.deepEqual(cells.slice(1, 5), ["1", "12M", "2.0M", "500k"], `${scenario} model row keeps output 2.0M and shows reasoning 500k`);
      await page.screenshot({ path: `${out}/${scenario}-task.png`, fullPage: true });
      const judgeNow = await get("intercept/judge/usage?days=30", token);
      await page.goto(`${base}/system/intercept`);
      await page.getByRole("tab", { name: texts[locale].judgeTab, exact: true }).click();
      const compact = n => new Intl.NumberFormat(locale, { notation: "compact", maximumFractionDigits: 1 }).format(n);
      const stat = label => page.getByText(label, { exact: true }).locator("xpath=..").textContent();
      await page.getByText(texts[locale].reasoning, { exact: true }).waitFor();
      assert.equal((await stat(texts[locale].judgeOutput)).trim(), `${texts[locale].judgeOutput}${compact(judgeNow.output_tokens)}`, `${scenario} judge output`);
      assert.equal((await stat(texts[locale].reasoning)).trim(), `${texts[locale].reasoning}${compact(judgeNow.reasoning_tokens)}`, `${scenario} judge reasoning`);
      await page.screenshot({ path: `${out}/${scenario}-judge.png`, fullPage: true });
      console.log(`PASS ${scenario}`);
    } catch (e) { failures.push(scenario); console.log(`FAIL ${scenario}: ${e.message}`); await page.screenshot({ path: `${out}/${scenario}-failed.png`, fullPage: true }); }
    await context.close();
  }
  assert.equal(errors.length, 0, `uncaught browser errors: ${errors.join(" | ")}`);
} catch (error) { bodyError = error; }
finally {
  await cleanup("browser close", async () => { if (browser) await browser.close(); });
  await cleanup("API disposal", () => api.dispose());
  await cleanup("owned server stop", stopServer);
  // Never delete records while our task handles are still live.
  if (!child || childResult) {
    const remove = (table, ids) => {
      if (!ids.length) return;
      sql(`DELETE FROM ${table} WHERE id IN (${ids.join(",")})`);
      assert.equal(sql(`SELECT count(*) FROM ${table} WHERE id IN (${ids.join(",")})`), "0", `${table} fixture cleanup read-back`);
    };
    await cleanup("usage fixtures", () => remove("llm_usage", ownedUsage));
    await cleanup("conversation fixtures", () => remove("conversations", ownedConversations));
    await cleanup("task fixture", () => remove("tasks", ownedTask ? [ownedTask.id] : []));
    await cleanup("exploration fixture", () => remove("explorations", ownedTask ? [ownedTask.exploration] : []));
    await cleanup("profile fixtures", () => remove("llm_profiles", Object.values(profiles)));
    await cleanup("owned runtime", () => { if (runtime) fs.rmSync(runtime, { recursive: true, force: true }); });
  }
}
if (bodyError || cleanupErrors.length) throw new AggregateError([...(bodyError ? [bodyError] : []), ...cleanupErrors], "dashboard regression or cleanup failed");
console.log(failures.length ? `Dashboard E2E failed: ${failures.length} scenario(s)` : "Dashboard E2E passed: 14 scenarios");
process.exitCode = failures.length ? 1 : 0;
