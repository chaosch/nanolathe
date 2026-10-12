// Loopback harness for the real WebGPU battle in a browser. Content is read
// lazily by HTTP range requests through the repository's BrowserFS adapter;
// console text and the battle's report files are posted back to serve-battle.
import {BrowserFS} from './fs.js';
import {BrowserStorage} from './storage.js';

const status = text => { document.querySelector('#status').textContent = text; };
const post = (path, body) => fetch(path, {method: 'POST', body});
const log = text => { console.log(text); return post('/log', `[${(performance.now() / 1000).toFixed(3)}] ${text}`); };

// A Blob-shaped view of one served file; fs.js reads only size, slice and arrayBuffer.
class RangeBlob {
  constructor(url, start, end) { this.url = url; this.start = start; this.end = end; }
  get size() { return this.end - this.start; }
  slice(a = 0, b = this.size) {
    return new RangeBlob(this.url, this.start + Math.min(a, this.size), this.start + Math.min(b, this.size));
  }
  async arrayBuffer() {
    if (this.size <= 0) return new ArrayBuffer(0);
    const response = await fetch(this.url, {headers: {Range: `bytes=${this.start}-${this.end - 1}`}});
    if (!response.ok) throw new Error(`range read failed: ${this.url} ${response.status}`);
    return response.arrayBuffer();
  }
}

// Record the adapter and every uncaptured WebGPU error the battle produces.
if (navigator.gpu) {
  const request = navigator.gpu.requestAdapter.bind(navigator.gpu);
  navigator.gpu.requestAdapter = async (...args) => {
    const adapter = await request(...args);
    if (!adapter) { log('webgpu: no adapter'); return adapter; }
    log('webgpu adapter ' + JSON.stringify({vendor: adapter.info?.vendor, architecture: adapter.info?.architecture,
      description: adapter.info?.description, features: [...adapter.features]}));
    const create = adapter.requestDevice.bind(adapter);
    adapter.requestDevice = async (...deviceArgs) => {
      const device = await create(...deviceArgs);
      device.addEventListener('uncapturederror', e => log('webgpu uncaptured error: ' + e.error.message));
      device.lost.then(info => log('webgpu device lost: ' + info.reason + ' ' + info.message));
      return device;
    };
    return adapter;
  };
} else {
  log('webgpu: navigator.gpu is absent');
}

let finished = false;
async function finish(result) {
  if (finished) return;
  finished = true;
  await post('/result/done.json', JSON.stringify(result));
  status('finished: ' + JSON.stringify(result));
}

try {
  const launch = await (await fetch('/launch.json')).json();
  const files = launch.files.map(f => [f.path, new RangeBlob('/game/' + f.path.split('/').map(encodeURIComponent).join('/'), 0, f.size)]);
  const storage = new BrowserStorage('nanolathe-webgpu-battle-probe');
  const install = new BrowserFS(files, storage, text => log(text), () => {});
  await install.restore();
  window.fs = install.fs; window.process = install.process; window.path = install.path;
  await new Promise((resolve, reject) => {
    const script = document.createElement('script');
    script.src = '/wasm_exec.js'; script.onload = resolve; script.onerror = () => reject(new Error('wasm_exec.js'));
    document.head.append(script);
  });
  const go = new Go();
  go.env = {HOME: '/settings', TMPDIR: '/tmp', NANOLATHE_SETTINGS: '/settings/settings.json', ...(launch.env || {})};
  go.argv = ['nanolathe', '--root=/game', '--save-dir=/saves', '--mod=none', '--fullscreen=false', '--auto-remaster=false', ...(launch.argv || [])];
  log('launch ' + JSON.stringify({argv: go.argv, env: go.env, files: files.length, dpr: devicePixelRatio,
    viewport: [innerWidth, innerHeight], userAgent: navigator.userAgent}));
  status('compiling…');
  const {instance} = await WebAssembly.instantiateStreaming(fetch('/nanolathe.wasm'), go.importObject);
  if (launch.timeoutMs) setTimeout(() => finish({timeout: true, ms: launch.timeoutMs}), launch.timeoutMs);
  status('running…');
  const started = performance.now();
  await go.run(instance);
  // Report files live in BrowserFS's in-memory /tmp; send them back as bytes.
  const sent = [];
  for (const [path, node] of install.nodes) {
    if (!path.startsWith('/tmp/report/') || node.dir || !node.bytes) continue;
    await post('/result/' + path.slice('/tmp/report/'.length).replaceAll('/', '_'), node.bytes);
    sent.push(path);
  }
  await finish({exited: true, seconds: (performance.now() - started) / 1000, sent});
} catch (error) {
  await log('harness error: ' + (error.stack || error.message));
  await finish({error: String(error.message || error)});
}
