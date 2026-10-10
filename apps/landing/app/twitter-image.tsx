import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { ImageResponse } from "next/og";

export const alt = "Sparkbase: your backend, on your own server";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

const KINDS = ["Postgres", "Cache", "Storage", "Vector", "Queues", "Auth", "Realtime", "Cron", "Backups", "Monitor"];

export default async function OpengraphImage() {
  const [bold, mark] = await Promise.all([readFile(join(process.cwd(), "assets", "Sora-Bold.ttf")), readFile(join(process.cwd(), "public", "mark.png"))]);
  return new ImageResponse(
    <div style={{ width: "100%", height: "100%", display: "flex", flexDirection: "column", justifyContent: "space-between", padding: 80, background: "#0A0A0B", fontFamily: "Sora" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 20 }}>
        <img src={`data:image/png;base64,${mark.toString("base64")}`} width={60} height={60} alt="" />
        <div style={{ display: "flex", fontSize: 52, fontWeight: 700, letterSpacing: -2, color: "#F5F5F2" }}>Sparkbase</div>
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 28 }}>
        <div style={{ display: "flex", flexDirection: "column", fontSize: 84, fontWeight: 700, lineHeight: 1.04, letterSpacing: -4, color: "#F5F5F2" }}>
          <div style={{ display: "flex" }}>Your backend,</div>
          <div style={{ display: "flex" }}>
            on your&nbsp;<span style={{ color: "#2D57FB" }}>own server.</span>
          </div>
        </div>
        <div style={{ display: "flex", flexWrap: "wrap", gap: 10 }}>
          {KINDS.map((k) => (
            <div key={k} style={{ display: "flex", border: "1px solid #26262C", borderRadius: 10, padding: "6px 14px", fontSize: 22, color: "#9A9AA2" }}>
              {k}
            </div>
          ))}
        </div>
      </div>
    </div>,
    { ...size, fonts: [{ name: "Sora", data: bold, weight: 700, style: "normal" }] },
  );
}
