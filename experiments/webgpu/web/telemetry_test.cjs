// Authored host-bookkeeping regression cases; mocks provide no GPU qualification evidence.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const nodes=new Map();function node(key){if(!nodes.has(key))nodes.set(key,{textContent:'',style:{},width:1920,height:1080,addEventListener(kind,fn){this[kind]=fn}});return nodes.get(key)}
const raw={features:new Set(),limits:{},queue:{submit(){},onSubmittedWorkDone(){return Promise.resolve()}},addEventListener(){},lost:new Promise(()=>{})};
const adapter={info:{},features:new Set(),async requestDevice(){return raw}};
let now=10;
const context=vm.createContext({navigator:{userAgent:'authored-test',gpu:{async requestAdapter(){return adapter}}},devicePixelRatio:2,performance:{now(){return now++}},document:{querySelector:node,addEventListener(){},fullscreenElement:null},window:{},Promise,queueMicrotask,URLSearchParams,location:{search:''},setTimeout});
context.window=context;vm.runInContext(fs.readFileSync(__dirname+'/harness.js','utf8'),context);
(async()=>{await context.navigator.gpu.requestAdapter();await adapter.requestDevice();node('canvas').keydown({key:'ArrowRight',timeStamp:1});context.probeBridge.markEncoded(7,1);raw.queue.submit([]);context.probeBridge.markEncoded(8,1);raw.queue.submit([]);raw.queue.submit([]);
let rows=vm.runInContext('report.submissions',context);assert.equal(rows[0].draw,7);assert.ok(rows[0].input_to_submission_ms>0);assert.equal(rows[1].input_to_submission_ms,null);assert.equal(rows[2].draw,-1);assert.equal(rows[2].input,0);assert.equal(rows[2].input_to_submission_ms,null);
for(let i=0;i<8200;i++)raw.queue.submit([]);await Promise.resolve();context.probeBridge.publish('{"test":true}');let report=JSON.parse(node('#report').textContent);assert.equal(report.submissions.length,8192);assert.equal(report.submissionCount,8203);assert.equal(report.completionHandles,0);console.log('browser telemetry bookkeeping regressions passed (mocked queue)');})().catch(e=>{console.error(e);process.exitCode=1});
