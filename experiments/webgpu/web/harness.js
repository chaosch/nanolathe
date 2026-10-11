"use strict";
const report = {events:[], device:null, adapter:null, results:[], userAgent:navigator.userAgent, dpr:devicePixelRatio};
const completions = new Map(); let token = 0;
let device;
function log(kind, value){report.events.push({kind,value,at:performance.now()});document.querySelector('#report').textContent=JSON.stringify(report,null,2);}
window.probeBridge = {
 lost:'', stop:false,
 beginCompletion(){const id=++token;device.queue.onSubmittedWorkDone().then(()=>completions.set(id,'done'),e=>completions.set(id,String(e)));return id;},
 completion(id){const value=completions.get(id);if(value!==undefined)completions.delete(id);return value;},
 publish(text){report.results.push(JSON.parse(text));log('finished',true);document.querySelector('#status').textContent='Smoke finished; inspect report.';},
};
if(navigator.gpu){
 const request=navigator.gpu.requestAdapter.bind(navigator.gpu);
 navigator.gpu.requestAdapter=async function(...args){
  const adapter=await request(...args);if(!adapter)return adapter;
  report.adapter={info:adapter.info,features:[...adapter.features]};
  const create=adapter.requestDevice.bind(adapter);
  adapter.requestDevice=async function(...args){
   device=await create(...args);
   report.device={features:[...device.features],limits:Object.fromEntries(Object.getOwnPropertyNames(Object.getPrototypeOf(device.limits)).filter(k=>k!=='constructor').map(k=>[k,device.limits[k]]))};
   device.addEventListener('uncapturederror',e=>log('uncaptured',e.error.message));
   device.lost.then(info=>{probeBridge.lost=info.message||info.reason;log('device-lost',{reason:info.reason,message:info.message});});
   log('device-created',true);return device;
  };return adapter;
 };
}else{document.querySelector('#status').textContent='WebGPU unavailable. Use a WebGPU-enabled desktop browser on localhost or HTTPS.';document.querySelector('#start').disabled=true;}
document.querySelector('#start').onclick=async()=>{
 document.querySelector('#start').disabled=true;
 try{const go=new Go();go.argv=['webgpu-probe','-frames=0'];const result=await WebAssembly.instantiateStreaming(fetch('probe.wasm'),go.importObject);await go.run(result.instance);}catch(e){log('runtime-error',String(e));}
};
document.querySelector('#resize').onclick=()=>{const c=document.querySelector('canvas');c.style.width='480px';c.style.height='360px';window.dispatchEvent(new Event('resize'));log('resize-request',{css:[480,360]});};
document.querySelector('#loss').onclick=()=>device?.destroy();
document.querySelector('#download').onclick=()=>{const a=document.createElement('a');a.href=URL.createObjectURL(new Blob([JSON.stringify(report,null,2)],{type:'application/json'}));a.download='webgpu-browser-report.json';a.click();setTimeout(()=>URL.revokeObjectURL(a.href),0);};
document.addEventListener('visibilitychange',()=>log('visibility',document.visibilityState));
document.querySelector('canvas').addEventListener('keydown',e=>log('input',{key:e.key,timestamp:e.timeStamp}));

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
