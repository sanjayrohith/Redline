import { NextRequest, NextResponse } from "next/server";
import { gatewayBaseURL } from "@/lib/config";

// Proxies GET/POST /v1/api-keys, forwarding the browser's session cookie
// upstream so the gateway can identify the principal - the browser itself
// only ever talks to this app's own origin.
export async function GET(req: NextRequest) {
  const res = await fetch(`${gatewayBaseURL()}/v1/api-keys`, {
    headers: { Cookie: req.headers.get("cookie") ?? "" },
  });
  return new NextResponse(await res.text(), {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}

export async function POST(req: NextRequest) {
  const body = await req.text();
  const res = await fetch(`${gatewayBaseURL()}/v1/api-keys`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Cookie: req.headers.get("cookie") ?? "",
    },
    body,
  });
  return new NextResponse(await res.text(), {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}
