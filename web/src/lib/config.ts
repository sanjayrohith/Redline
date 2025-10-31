// Server-side gateway base URL (never sent to the browser). The browser
// instead calls this Next.js app's own /api/* route handlers, which proxy
// to the gateway using this value plus any httpOnly session cookie -
// keeping the gateway's origin and any bearer credentials off the client.
export function gatewayBaseURL(): string {
  return process.env.REDLINE_GATEWAY_URL ?? "http://localhost:8080";
}
