const express = require('express');
const { WebSocketServer } = require('ws');
const { v4: uuidv4 } = require('uuid');
const http = require('http');

const app = express();
app.use(express.static('public'));

const server = http.createServer(app);
const wss = new WebSocketServer({ server });

const hosts = new Map();
const clients = new Map();

wss.on('connection', (ws) => {
  ws.id = uuidv4();

  ws.on('message', (msg) => {
    try {
      const data = JSON.parse(msg);

      if (data.type === 'register_host') {
        ws.hostId = data.id;
        hosts.set(data.id, ws);
        console.log(`Host registrado: ${data.id}`);
        return;
      }

      if (data.type === 'connect_client') {
        const host = hosts.get(data.hostId);
        if (host) {
          clients.set(ws.id, ws);
          ws.hostId = data.hostId;
          ws.send(JSON.stringify({ type: 'connected' }));
          console.log(`Cliente conectado ao host: ${data.hostId}`);
        } else {
          ws.send(JSON.stringify({ type: 'waiting', msg: 'Host não encontrado' }));
        }
        return;
      }

      if (data.type === 'control') {
        const host = hosts.get(data.hostId);
        if (host) host.send(JSON.stringify({ type: 'control', payload: data.payload }));
      }
    } catch (e) {
      console.error('Erro parseando msg:', e);
    }
  });

  ws.on('close', () => {
    if (ws.hostId && hosts.get(ws.hostId) === ws) hosts.delete(ws.hostId);
    if (clients.has(ws.id)) clients.delete(ws.id);
  });
});

const PORT = process.env.PORT || 3000;
server.listen(PORT, () => console.log(`Servidor rodando em http://localhost:${PORT}`));
