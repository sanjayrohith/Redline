import { NextRequest, NextResponse } from "next/server";
import { gatewayBaseURL } from "@/lib/config";

export async function GET(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const res = await fetch(`${gatewayBaseURL()}/v1/dashboard/deployments/${encodeURIComponent(id)}/session`, {
    headers: { Cookie: req.headers.get("cookie") ?? "" },
  });
  return new NextResponse(await res.text(), {
    status: res.status,
    headers: { "Content-Type": "application/json" },
  });
}
