// Installed by the browser context before app scripts. Only configured APIs
// are replaced, and only in the web app's origin. No real-model fallback.
function installModelStubs(artifacts, origin) {
  if (globalThis.location && (globalThis.origin || location.origin) !== origin) return;
  const encoder = new TextEncoder();
  const word = b => b >= 128 || b >= 48 && b <= 57 || b >= 65 && b <= 90 || b >= 97 && b <= 122;
  const has = (text, key) => {
    const a = encoder.encode(text), b = encoder.encode(key);
    for (let p = 0; p + b.length <= a.length; ++p) {
      let equal = true;
      for (let j = 0; j < b.length; ++j) if (a[p+j] !== b[j]) { equal = false; break; }
      const end = p + b.length;
      if (equal && (!p || !word(a[p-1]) || !word(b[0])) &&
          (end === a.length || !word(a[end]) || !word(b[b.length-1]))) return true;
    }
    return false;
  };
  const textOf = value => {
    if (typeof value === 'string') return value;
    if (Array.isArray(value)) return value.map(textOf).join('\n');
    if (value && typeof value === 'object') {
      if (value.type && value.type !== 'text') throw new DOMException('This fixture accepts text input', 'NotSupportedError');
      if ('content' in value) return textOf(value.content);
      if ('value' in value) return textOf(value.value);
    }
    throw new TypeError('Expected text or text messages');
  };
  const answer = (artifact, text) => {
    const i = (artifact.texts || []).findIndex(key => has(text, key));
    return artifact.generated[i < 0 ? artifact.generated.length - 1 : i];
  };
  const abort = signal => { if (signal?.aborted) throw signal.reason ?? new DOMException('Aborted', 'AbortError'); };
  // The page's own microphone: getUserMedia's audio plays the recordings the
  // microphone step queues, each once, in order, then silence. A recognizer
  // started on a track of it answers the recording the track plays.
  // The stub is the prototype's: WebKit may collect navigator.mediaDevices'
  // wrapper, with what is set on it, when no script holds it.
  const captures = new WeakMap();
  const Media = globalThis.MediaDevices?.prototype;
  if (Media?.getUserMedia && globalThis.__axxTakeRecording) {
    const real = Media.getUserMedia;
    const getUserMedia = async function (constraints) {
      if (!constraints?.audio) return real.call(this, constraints);
      const context = new AudioContext();
      const destination = context.createMediaStreamDestination();
      const stream = destination.stream;
      const [track] = stream.getAudioTracks();
      const state = {recording: null, waiters: [], stopped: false};
      captures.set(track, state);
      const stop = track.stop.bind(track);
      track.stop = () => { state.stopped = true; stop(); context.close().catch(() => {}); };
      await context.resume().catch(() => {});
      (async () => {
        while (!state.stopped) {
          const recording = await globalThis.__axxTakeRecording();
          if (state.stopped) return;
          if (!recording || recording.error) { await new Promise(resolve => setTimeout(resolve, 25)); continue; }
          const buffer = context.createBuffer(1, Math.max(1, recording.samples.length), recording.rate);
          buffer.copyToChannel(Float32Array.from(recording.samples), 0);
          const source = context.createBufferSource();
          source.buffer = buffer;
          source.connect(destination);
          state.recording = recording;
          for (const resolve of state.waiters.splice(0)) resolve(recording);
          await new Promise(resolve => { source.onended = resolve; source.start(); });
        }
      })();
      if (constraints.video) {
        const video = await real.call(this, {video: constraints.video});
        for (const t of video.getVideoTracks()) stream.addTrack(t);
      }
      return stream;
    };
    Object.defineProperty(Media, 'getUserMedia', {value: getUserMedia, configurable: true, writable: true});
  }
  // The recording a track of the page's microphone plays, other than the one
  // last heard: the one playing now, or the next.
  const playing = (track, last) => {
    const state = captures.get(track);
    if (state.recording && state.recording !== last) return Promise.resolve(state.recording);
    return new Promise(resolve => state.waiters.push(resolve));
  };
  const speech = artifacts.find(a => a.api === 'web.speech');
  if (speech) {
    const input = speech.contract.inputs.find(i => !speech.contract.match?.input || i.name === speech.contract.match.input);
    const features = !!input.audio.features;
    const n = features ? 256 : input.shape.reduce((a,b) => a*b, 1), rate = input.audio.sampleRate;
    let frame = 0;
    for (let f=100; f<=1024 && f<=n; ++f) if (n%f === 0 && (!frame || Math.abs(f-256)<Math.abs(frame-256))) frame=f;
    const bins=Math.floor(frame/2), frames=n/frame, cos=new Float64Array(frame*bins), sin=new Float64Array(frame*bins);
    const band=new Uint8Array(bins), mel=f => 2595*Math.log10(1+f/700), lo=mel(rate/frame), hi=mel(rate/2);
    for(let i=0;i<frame;++i) for(let j=0;j<bins;++j) {
      const a=2*Math.PI*((j+1)*i%frame)/frame;cos[i*bins+j]=Math.cos(a);sin[i*bins+j]=Math.sin(a);
    }
    for(let j=0;j<bins;++j) band[j]=Math.max(0,Math.min(31,Math.floor((mel((j+1)*rate/frame)-lo)/(hi-lo)*32)));
    const recognize = recording => {
      // axx matches a recording with its reference when the microphone step
      // queues it; a stub of features is matched only so.
      const matched = recording.matches?.['web.speech'];
      if (Number.isInteger(matched) && matched >= 0 && matched < speech.generated.length) return speech.generated[matched];
      if (features) throw new Error('The recording was not matched to this speech stub; rebuild the stub and queue the recording again');
      if(recording.rate!==rate) throw new Error('Recording sample rate differs from the contract');
      const v=new Float64Array(32), samples=recording.samples;
      for(let f=0;f<frames;++f) for(let j=0;j<bins;++j) {
        let re=0,im=0;
        for(let i=0;i<frame;++i) {const x=samples[f*frame+i] || 0;re+=x*cos[i*bins+j];im+=x*sin[i*bins+j];}
        v[band[j]]+=re*re+im*im;
      }
      let mean=0,variance=0;
      for(let b=0;b<32;++b){v[b]=Math.log(v[b]/frames+1e-10);mean+=v[b];}mean/=32;
      for(let b=0;b<32;++b){v[b]-=mean;variance+=v[b]*v[b];}
      const sd=Math.sqrt(variance/32+1e-12);for(let b=0;b<32;++b)v[b]/=sd;
      const keys=speech.keys || [];let at=keys.length,best=speech.similarity;
      for(let i=keys.length-1;i>=0;--i) {let dot=0;for(let j=0;j<32;++j)dot+=v[j]*keys[i][j];if(dot/32>=best){best=dot/32;at=i;}}
      return speech.generated[at];
    };
    class SpeechRecognition extends EventTarget {
      lang=''; continuous=false; interimResults=false; maxAlternatives=1; processLocally=true;
      #active=false; #generation=0; #results=[];
      #emit(type, fields={}) {const event=new Event(type);Object.assign(event,fields);this.dispatchEvent(event);}
      start(track) {
        if(track!==undefined && !captures.has(track)) throw new DOMException('This adapter takes microphone-step recordings, or a track of the page\'s microphone', 'NotSupportedError');
        if(this.#active) throw new DOMException('Recognition has started', 'InvalidStateError');
        this.#active=true;this.#results=[];const generation=++this.#generation;
        setTimeout(async () => {
          if(!this.#active || this.#generation!==generation)return;
          this.#emit('start');this.#emit('audiostart');
          try {
            let last=null;
            while(this.#active && this.#generation===generation) {
              const recording=track!==undefined ? (last=await playing(track,last)) : await globalThis.__axxTakeRecording();
              if(!this.#active || this.#generation!==generation)return;
              if(recording?.error) throw new Error(recording.error);
              if(!recording){await new Promise(resolve=>setTimeout(resolve,25));continue;}
              const transcript=recognize(recording), alternative={transcript,confidence:1};
              this.#emit('soundstart');this.#emit('speechstart');
              const result={0:alternative,length:1,isFinal:true,item(i){return this[i] ?? null;}};
              const resultIndex=this.#results.length;this.#results.push(result);
              const results=Object.assign({length:this.#results.length,item(i){return this[i] ?? null;}},this.#results);
              this.#emit('result',{resultIndex,results});this.#emit('speechend');this.#emit('soundend');
              if(!this.continuous){this.stop();return;}
            }
          } catch(error) {if(this.#active){this.#emit('error',{error:'audio-capture',message:error.message});this.stop();}}
        },0);
      }
      stop(){if(!this.#active)return;this.#active=false;++this.#generation;this.#emit('audioend');this.#emit('end');}
      abort(){if(!this.#active)return;this.#emit('error',{error:'aborted',message:'Aborted'});this.stop();}
      static async available(){return 'available';}
      static async install(){return true;}
    }
    for(const event of ['start','audiostart','soundstart','speechstart','result','speechend','soundend','audioend','end','error','nomatch']) {
      const key=Symbol(event);
      Object.defineProperty(SpeechRecognition.prototype,'on'+event,{get(){return this[key] || null;},set(fn){if(this[key])this.removeEventListener(event,this[key]);this[key]=typeof fn==='function'?fn:null;if(this[key])this.addEventListener(event,this[key]);},configurable:true});
    }
    Object.defineProperty(globalThis,'SpeechRecognition',{value:SpeechRecognition,configurable:true,writable:true});
    Object.defineProperty(globalThis,'webkitSpeechRecognition',{value:SpeechRecognition,configurable:true,writable:true});
  }
  const token = Symbol('create');
  for (const artifact of artifacts) {
    const spec = {
      'web.language-model': ['LanguageModel', 'prompt'],
      'web.summarizer': ['Summarizer', 'summarize'],
      'web.writer': ['Writer', 'write'],
      'web.rewriter': ['Rewriter', 'rewrite'],
      'web.translator': ['Translator', 'translate'],
      'web.language-detector': ['LanguageDetector', 'detect'],
      'web.proofreader': ['Proofreader', 'proofread'],
    }[artifact.api];
    if (!spec) continue;
    const [name, method] = spec;
    const conversational = name === 'LanguageModel';
    const streams = name !== 'LanguageDetector' && name !== 'Proofreader';
    // A mapping's answer as the API gives it: a detector's languages and a
    // proofreader's corrections are its JSON, or are made of its text.
    const json = text => { try { return JSON.parse(text); } catch { return undefined; } };
    const shape = {
      LanguageDetector: text => {
        const v = json(text);
        return Array.isArray(v) ? v : [{detectedLanguage: text, confidence: 1}];
      },
      Proofreader: (text, input) => {
        const v = json(text);
        if (v && typeof v === 'object' && !Array.isArray(v) && typeof v.correctedInput === 'string') return v;
        let start = 0, end = 0;
        while (start < input.length && start < text.length && input[start] === text[start]) ++start;
        while (end < input.length - start && end < text.length - start && input[input.length - 1 - end] === text[text.length - 1 - end]) ++end;
        const corrections = text === input ? [] : [{startIndex: start, endIndex: input.length - end, correction: text.slice(start, text.length - end)}];
        return {correctedInput: text, corrections};
      },
    }[name] || (text => text);
    // The options an app reads back from its instance.
    const kept = {
      Translator: ['sourceLanguage', 'targetLanguage'],
      LanguageDetector: ['expectedInputLanguages'],
      Proofreader: ['expectedInputLanguages', 'includeCorrectionTypes', 'includeCorrectionExplanations', 'correctionExplanationLanguage'],
    }[name] || [];
    class Model extends EventTarget {
      #history;
      #options;
      #destroyed = false;
      #pending = new Set();
      constructor(key, options = {}) {
        super();
        if (key !== token) throw new TypeError('Illegal constructor; use create()');
        this.#options = {...options};
        this.#history = options.initialPrompts ? [textOf(options.initialPrompts)] : [];
      }
      static async availability(options = {}) { abort(options.signal); return 'available'; }
      static async create(options = {}) {
        abort(options.signal);
        return new Model(token, options);
      }
      #check(options) {
        abort(options.signal);
        if (this.#destroyed) throw new DOMException('The model was destroyed', 'InvalidStateError');
      }
      async _respond(value, options = {}) {
        this.#check(options);
        const input = textOf(value);
        await Promise.resolve();
        this.#check(options);
        const context = [this.#options.sharedContext || '', options.context || '', ...this.#history, input].join('\n');
        const result = answer(artifact, context);
        if (conversational) this.#history.push(input, result);
        return shape(result, input);
      }
      _option(key) { return this.#options[key]; }
      _stream(value, options = {}) {
        this.#check(options);
        const controller = new AbortController();
        const forward = () => controller.abort(options.signal.reason);
        options.signal?.addEventListener('abort', forward, {once: true});
        this.#pending.add(controller);
        const clean = () => { this.#pending.delete(controller); options.signal?.removeEventListener('abort', forward); };
        return new ReadableStream({
          start: stream => {
            this._respond(value, {...options, signal: controller.signal}).then(result => {
              if (!controller.signal.aborted) { stream.enqueue(result); stream.close(); }
            }, err => stream.error(err)).finally(clean);
          },
          cancel: () => { controller.abort(); clean(); },
        });
      }
      async clone(options = {}) {
        this.#check(options);
        const copy = new Model(token, this.#options);
        copy.#history = [...this.#history];
        return copy;
      }
      async append(value, options = {}) { this.#check(options); this.#history.push(textOf(value)); }
      destroy() {
        this.#destroyed = true;
        for (const controller of this.#pending) controller.abort(new DOMException('The model was destroyed', 'InvalidStateError'));
        this.#pending.clear();
      }
    }
    Object.defineProperty(Model, 'name', {value: name});
    Object.defineProperty(Model.prototype, method, {value: Model.prototype._respond, configurable: true});
    if (streams) Object.defineProperty(Model.prototype, method + 'Streaming', {value: Model.prototype._stream, configurable: true});
    for (const key of kept) Object.defineProperty(Model.prototype, key, {get() { return this._option(key); }, configurable: true});
    // The task APIs have independent calls; only LanguageModel has conversation management.
    if (!conversational) { delete Model.prototype.clone; delete Model.prototype.append; }
    Object.defineProperty(globalThis, name, {value: Model, configurable: true, writable: true});
  }
}
