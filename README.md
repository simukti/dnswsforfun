### DNS And Websocket For Fun

A toy server that serve DNS and HTTP server (with websocket endpoint) to broadcast incoming DNS request to the
websocket.

- This server provides a DNS server on UDP port 53, each incoming DNS request will be broadcasted to all connected
  websocket clients. To demonstrate the server-to-client message.
- It also receives an incoming free text message from the WebSocket client and then the incoming message will also be
  broadcasted to all connected websocket clients. To demonstrate the client-to-server-to-client message.

_This repo is a sample codes for sharing session to cover websocket, and concurrency (goroutine and channel)_