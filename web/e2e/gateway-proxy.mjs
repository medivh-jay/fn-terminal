// Exercise the actual browser through the Host rewrite observed on fnOS.
import http from 'node:http';

const target = (request) => ({
  hostname: '127.0.0.1',
  port: 18088,
  method: request.method,
  path: request.url,
  headers: { ...request.headers, host: 'localhost' },
});
const server = http.createServer((request, response) => {
  if (request.url === '/foreign-page') {
    response.writeHead(200, { 'Content-Type': 'text/html' });
    response.end('<!doctype html><title>Foreign site</title>');
    return;
  }
  const upstream = http.request(target(request), (res) => {
    response.writeHead(res.statusCode, res.headers);
    res.pipe(response);
  });
  upstream.on('error', () => {
    response.writeHead(502);
    response.end();
  });
  request.pipe(upstream);
});
server.on('upgrade', (request, socket, head) => {
  const upstream = http.request(target(request));
  upstream.on('upgrade', (response, peer, peerHead) => {
    const headers = Object.entries(response.headers)
      .map(([key, value]) => `${key}: ${value}\r\n`)
      .join('');
    socket.write(`HTTP/1.1 101 Switching Protocols\r\n${headers}\r\n`);
    if (peerHead.length) socket.write(peerHead);
    if (head.length) peer.write(head);
    socket.on('error', () => peer.destroy());
    peer.on('error', () => socket.destroy());
    socket.on('close', () => peer.destroy());
    peer.on('close', () => socket.destroy());
    peer.pipe(socket).pipe(peer);
  });
  upstream.on('response', (response) => {
    response.resume();
    socket.end(
      `HTTP/1.1 ${response.statusCode} Rejected\r\nContent-Length: 0\r\nConnection: close\r\n\r\n`,
    );
  });
  upstream.on('error', () => socket.destroy());
  upstream.end();
});
server.listen(18089, '127.0.0.1');
