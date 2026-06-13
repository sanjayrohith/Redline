// Server-side gateway base URL (never sent to the browser). The browser
// instead calls this Next.js app's own /api/* route handlers, which proxy
// to the gateway using this value plus any httpOnly session cookie -
// keeping the gateway's origin and any bearer credentials off the client.
export function gatewayBaseURL(): string {
  return process.env.REDLINE_GATEWAY_URL ?? "http://localhost:8080";
}

// Public gateway WebSocket origin. Unlike gatewayBaseURL, this one is
// necessarily browser-visible: a WebSocket connects directly from the
// page to the gateway (see web/src/app/api/dashboard/ws-ticket/route.ts
// for why that connection cannot instead go through this app's own
// proxy). NEXT_PUBLIC_-prefixed variables are inlined into the client
// bundle at build time by Next.js.
export function gatewayWSBaseURL(): string {
  return process.env.NEXT_PUBLIC_GATEWAY_WS_URL ?? "ws://localhost:8080";
}
