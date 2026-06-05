import { NextRequest, NextResponse } from "next/server";
import { gatewayBaseURL } from "@/lib/config";

export async function DELETE(req: NextRequest, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const res = await fetch(`${gatewayBaseURL()}/v1/api-keys/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: { Cookie: req.headers.get("cookie") ?? "" },
  });
  return new NextResponse(res.ok ? null : await res.text(), { status: res.status });
}
