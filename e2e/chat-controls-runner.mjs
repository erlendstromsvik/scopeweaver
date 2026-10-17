import assert from "node:assert/strict";
import {chromium,request} from "playwright";
import {runChatQueueChecks,runChatHistoryChecks,waitChatState} from "./chat-controls.mjs";
import {runChatPasteChecks} from "./chat-paste.mjs";
import {runChatContextChecks,runChatRuntimeChecks} from "./chat-context.mjs";
import {runChatDraftRetentionChecks} from "./chat-draft-retention.mjs";

const base=process.env.SCOPEWEAVER_E2E_URL;assert.ok(base?.startsWith("http://127.0.0.1:"),"loopback test listener required");
const api=await request.newContext({baseURL:base});
const init=await api.post("/api/auth/init",{data:{password:"chat-controls-pass-1234"}});assert.ok(init.ok(),await init.text());
const login=await api.post("/api/auth/login",{data:{username:"ARTEX",password:"chat-controls-pass-1234"}});assert.ok(login.ok(),await login.text());const {token}=await login.json();
const fixtureRequest=async(action,body)=>{const response=await api.post(`/__fixture/${action}`,{headers:{"X-Chat-Fixture":process.env.SCOPEWEAVER_E2E_FIXTURE},data:body??{}});assert.ok(response.ok(),await response.text());return response.json()};
const fixture={resetTools:()=>fixtureRequest("reset-tools"),holdNext:()=>fixtureRequest("hold"),release:()=>fixtureRequest("release"),calls:async()=>(await fixtureRequest("stats")).calls,requests:async()=>(await fixtureRequest("stats")).requests,stats:()=>fixtureRequest("stats"),setBudget:window=>fixtureRequest("budget",{window}),seedPendingApproval:async(ref,run,turn)=>(await fixtureRequest("approval",{ref,run,turn})).id,clearPendingApproval:id=>fixtureRequest("clear-approval",{id})};
const browser=await chromium.launch();const errors=[];const pages=[];let failed=0;
const helpers={base,token,fixture,check:assert.ok,step:async(name,fn)=>{try{await fn();console.log(`PASS ${name}`)}catch(error){failed++;console.error(`FAIL ${name}: ${error.stack}`);if(process.env.CHAT_QUEUE_RED||process.env.CHAT_CONTEXT_MUTATION)throw error}},openPage:async(locale,jwt)=>{const context=await browser.newContext();context.setDefaultTimeout(15000);await context.addInitScript(([locale,jwt])=>{localStorage.setItem("scopeweaver.locale",locale);localStorage.setItem("artex_token",jwt)},[locale,jwt]);const p=await context.newPage();p.on("pageerror",error=>errors.push(error.message));pages.push(p);return p}};
export async function runChatControlsChecks({base,token,restart,fixture,step,check,openPage}){
 const options={base,token,fixture,step,check,openPage};const only=process.env.CHAT_E2E_SUITE;const standard=!only||only==="standard";
 if(!only||only==="draft")await runChatDraftRetentionChecks(options);
 if(standard||only==="queue")await runChatQueueChecks({...options,recoverInput:restart});
 if(standard||only==="history")await runChatHistoryChecks(options);
 if(standard||only==="context")await runChatContextChecks(options);
 if(standard||only==="runtime")await runChatRuntimeChecks(options);
 if(standard||only==="paste")await runChatPasteChecks(options);
}

try{await runChatControlsChecks({...helpers,restart:id=>fixtureRequest("recover",{input_id:id})});assert.deepEqual(errors,[],"uncaught browser errors");console.log(`Chat browser gate: ${failed===0?"PASS":"FAIL"}`)}finally{await fixture.release();await browser.close();await api.dispose()}
if(failed)process.exitCode=1;
