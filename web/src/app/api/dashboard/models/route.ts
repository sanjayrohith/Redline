import { NextRequest, NextResponse } from "next/server";
import { gatewayBaseURL } from "@/lib/config";

export async function GET(req: NextRequest) {
  const res = await fetch(`${gatewayBaseURL()}/v1/dashboard/models`, {
    headers: { Cookie: req.headers.get("cookie") ?? "" },
  });
  return new NextResponse(await res.text(), {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}
