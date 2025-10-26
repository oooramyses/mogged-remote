(() => {
  const statusEl = document.getElementById('status');
  const img = document.getElementById('screen');
  const hostInput = document.getElementById('host');
  const connectBtn = document.getElementById('connect');
  const disconnectBtn = document.getElementById('disconnect');
  const logEl = document.getElementById('log');

  let ws = null;
  let hostId = null;

  function log(msg){ logEl.textContent += msg+'\n'; logEl.scrollTop = logEl.scrollHeight; }
  function setStatus(s){ statusEl.textContent = s; }

  function connectWS(){
    ws = new WebSocket('ws://localhost:3000');
    ws.binaryType = 'arraybuffer';

    ws.onopen = () => {
      setStatus('conectando...');
      ws.send(JSON.stringify({ type:'connect_client', hostId }));
      log('Conexão aberta');
    };

    ws.onmessage = (e) => {
      if(typeof e.data === 'string'){
        try {
          const msg = JSON.parse(e.data);
          if(msg.type === 'connected') setStatus('conectado');
          if(msg.type === 'waiting') setStatus(msg.msg || 'aguardando host');
          if(msg.type === 'error') setStatus('erro: '+msg.msg);
        } catch{}
        return;
      }
      const blob = new Blob([e.data], { type: 'image/jpeg' });
      const url = URL.createObjectURL(blob);
      img.src = url;
      setTimeout(()=>URL.revokeObjectURL(url),1500);
    };

    ws.onclose = () => { setStatus('desconectado'); log('Socket fechado'); };
    ws.onerror = (err) => { setStatus('erro'); log('Erro WS'); console.error(err); };
  }

  connectBtn.addEventListener('click', ()=>{
    hostId = hostInput.value.trim();
    if(!hostId) return alert('Insira Host ID');
    connectWS();
  });

  disconnectBtn.addEventListener('click', ()=>{
    if(ws) ws.close();
    ws=null;
    setStatus('offline');
  });

  function sendControl(obj){ if(!ws || ws.readyState!==1) return; ws.send(JSON.stringify({ type:'control', hostId, payload: obj })); }

  img.addEventListener('mousemove',(e)=>{
    if(!img.naturalWidth) return;
    const rect = img.getBoundingClientRect();
    const x = Math.round((e.clientX - rect.left)*(img.naturalWidth/rect.width));
    const y = Math.round((e.clientY - rect.top)*(img.naturalHeight/rect.height));
    sendControl({ type:'mouse', action:'move', x, y });
  });

  img.addEventListener('click',(e)=>{
    const rect = img.getBoundingClientRect();
    const x = Math.round((e.clientX - rect.left)*(img.naturalWidth/rect.width));
    const y = Math.round((e.clientY - rect.top)*(img.naturalHeight/rect.height));
    sendControl({ type:'mouse', action:'click', x, y, button:'left' });
  });

  img.addEventListener('contextmenu',(e)=>{
    e.preventDefault();
    const rect = img.getBoundingClientRect();
    const x = Math.round((e.clientX - rect.left)*(img.naturalWidth/rect.width));
    const y = Math.round((e.clientY - rect.top)*(img.naturalHeight/rect.height));
    sendControl({ type:'mouse', action:'click', x, y, button:'right' });
  });

  img.addEventListener('wheel',(e)=>{
    sendControl({ type:'mouse', action:'wheel', delta: Math.sign(e.deltaY) });
    e.preventDefault();
  },{passive:false});

  window.addEventListener('keydown',(e)=>{
    if(['Shift','Control','Alt','Meta'].includes(e.key)) return;
    sendControl({ type:'key', action:'tap', key:e.key });
  });
})();
