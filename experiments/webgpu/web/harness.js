"use strict";
const report = {events:[], device:null, adapter:null, results:[], submissions:[], maxOutstanding:0, completionHandles:0, submissionCount:0, userAgent:navigator.userAgent, dpr:devicePixelRatio};
const completions = new Map(); let token = 0;
let device;let marker=null,lastInput=null,lastReflectedInput=0,outstanding=0;
const submissionCapacity=8192,eventCapacity=1024;
function orderedReport(){return {...report,submissions:report.submissionCount<=submissionCapacity?report.submissions:report.submissions.slice(report.submissionCount%submissionCapacity).concat(report.submissions.slice(0,report.submissionCount%submissionCapacity))};}
function log(kind, value){if(report.events.length===eventCapacity)report.events.shift();report.events.push({kind,value,at:performance.now()});document.querySelector('#report').textContent=JSON.stringify(report,null,2);}
window.probeBridge = {
 lost:'', stop:false,
 measurement(text){report.measurement=JSON.parse(text);},
 markEncoded(draw,input){marker={draw,input};},
 capture(name,data){const image=document.createElement('img');image.alt=name;image.src='data:image/png;base64,'+data;image.style.width='480px';document.querySelector('#captures').append(image);},
 beginCompletion(){const id=++token;device.queue.onSubmittedWorkDone().then(()=>completions.set(id,'done'),e=>completions.set(id,String(e)));return id;},
 beginFrameCompletion(){const id=++token;queueMicrotask(()=>device.queue.onSubmittedWorkDone().then(()=>completions.set(id,'done'),e=>completions.set(id,String(e))));return id;},
 completion(id){const value=completions.get(id);if(value!==undefined)completions.delete(id);return value;},
 publish(text){report.completionHandles=completions.size;report.results.push(JSON.parse(text));log('finished',true);document.querySelector('#status').textContent='Smoke finished; inspect report.';},
};
if(navigator.gpu){
 const request=navigator.gpu.requestAdapter.bind(navigator.gpu);
 navigator.gpu.requestAdapter=async function(...args){
  const adapter=await request(...args);if(!adapter)return adapter;
  report.adapter={info:Object.fromEntries(["vendor","architecture","device","description","isFallbackAdapter"].map(k=>[k,adapter.info[k]])),features:[...adapter.features]};
  const create=adapter.requestDevice.bind(adapter);
  adapter.requestDevice=async function(...args){
   device=await create(...args);
   report.device={features:[...device.features],limits:Object.fromEntries(Object.getOwnPropertyNames(Object.getPrototypeOf(device.limits)).filter(k=>k!=='constructor').map(k=>[k,device.limits[k]]))};
   device.addEventListener('uncapturederror',e=>{probeBridge.lost='validation: '+e.error.message;log('uncaptured',e.error.message);});
   device.lost.then(info=>{probeBridge.lost=info.message||info.reason;log('device-lost',{reason:info.reason,message:info.message});});
   const submit=device.queue.submit.bind(device.queue);
   device.queue.submit=function(...args){const start=performance.now();submit(...args);const end=performance.now();const reflected=marker;marker=null;let latency=null;if(reflected&&lastInput&&reflected.input>lastReflectedInput){latency=end-lastInput.timestamp;lastReflectedInput=reflected.input;}const item={width:document.querySelector("canvas").width,height:document.querySelector("canvas").height,dpr:devicePixelRatio,draw:reflected?.draw??-1,input:reflected?.input??0,submit_start_ms:start,submit_end_ms:end,submit_cpu_ms:end-start,completion_ms:null,input_to_submission_ms:latency};report.submissions[report.submissionCount%submissionCapacity]=item;report.submissionCount++;outstanding++;report.maxOutstanding=Math.max(report.maxOutstanding,outstanding);device.queue.onSubmittedWorkDone().then(()=>{item.completion_ms=performance.now();outstanding--;},e=>log('queue-completion-error',String(e)));};
   log('device-created',true);return device;
  };return adapter;
 };
}else{document.querySelector('#status').textContent='WebGPU unavailable. Use a WebGPU-enabled desktop browser on localhost or HTTPS.';document.querySelector('#start').disabled=true;}
document.querySelector('#start').onclick=async()=>{
 document.querySelector('#start').disabled=true;
 try{const go=new Go();const params=new URLSearchParams(location.search);go.argv=['webgpu-probe','-frames=0'];for(const key of ['workload','qualify','count','detail','frames','cycles','mode','width','height','measure','physical-width','physical-height','refresh'])if(params.has(key))go.argv.push('-'+key+'='+params.get(key));const result=await WebAssembly.instantiateStreaming(fetch('probe.wasm'),go.importObject);await go.run(result.instance);}catch(e){log('runtime-error',String(e));}
};
document.querySelector('#resize').onclick=()=>{const c=document.querySelector('canvas');c.style.width='480px';c.style.height='360px';window.dispatchEvent(new Event('resize'));log('resize-request',{css:[480,360]});};
document.querySelector('#loss').onclick=()=>device?.destroy();
document.querySelector('#download').onclick=()=>{const a=document.createElement('a');a.href=URL.createObjectURL(new Blob([JSON.stringify(orderedReport(),null,2)],{type:'application/json'}));a.download='webgpu-browser-report.json';a.click();setTimeout(()=>URL.revokeObjectURL(a.href),0);};
document.addEventListener('visibilitychange',()=>log('visibility',document.visibilityState));
document.querySelector('canvas').addEventListener('keydown',e=>{lastInput={key:e.key,timestamp:e.timeStamp};log('input',lastInput);});

document.querySelector('#errors').onclick=async()=>{
 try{
  device.pushErrorScope('validation');
  const shader=device.createShaderModule({code:'@compute @workgroup_size(1) fn invalid() { let broken = ; }'});
  device.createComputePipeline({layout:'auto',compute:{module:shader,entryPoint:'invalid'}});
  const compile=await shader.getCompilationInfo();log('invalid-shader',compile.messages.map(x=>({type:x.type,message:x.message})));
  log('shader-scope',(await device.popErrorScope())?.message||null);
  device.pushErrorScope('validation');
  const layout=device.createBindGroupLayout({entries:[{binding:0,visibility:GPUShaderStage.COMPUTE,buffer:{type:'storage',minBindingSize:32}}]});
  const buffer=device.createBuffer({size:16,usage:GPUBufferUsage.STORAGE});
  device.createBindGroup({layout,entries:[{binding:0,resource:{buffer,size:16}}]});
  log('invalid-binding',(await device.popErrorScope())?.message||null);buffer.destroy();
 }catch(e){log('diagnostics-error',String(e));}
};
document.querySelector('#stop').onclick=()=>{probeBridge.stop=true;};

document.querySelector('#fullscreen').onclick=async()=>{try{if(document.fullscreenElement)await document.exitFullscreen();else await document.querySelector('canvas').requestFullscreen();log('fullscreen',!!document.fullscreenElement);}catch(e){log('fullscreen-error',String(e));}};
document.addEventListener('fullscreenchange',()=>log('fullscreen-change',!!document.fullscreenElement));
