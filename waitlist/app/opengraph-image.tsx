import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { ImageResponse } from "next/og";

export const alt = "Sparkbase: The data layer for your backend";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

export default async function OpengraphImage() {
  const [bold, mark] = await Promise.all([
    readFile(join(process.cwd(), "assets", "Sora-Bold.ttf")),
    readFile(join(process.cwd(), "public", "brand", "sparkbase-mark.png")),
  ]);
  const markSrc = `data:image/png;base64,${mark.toString("base64")}`;

  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          padding: 88,
          background: "#0A0A0B",
          fontFamily: "Sora",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 22 }}>
          <img src={markSrc} width={68} height={68} alt="" />
          <div style={{ display: "flex", fontSize: 60, fontWeight: 700, letterSpacing: -2 }}>
            <span style={{ color: "#F5F5F2" }}>Spark</span>
            <span style={{ color: "#2D57FB" }}>base</span>
          </div>
        </div>
        <div
          style={{
            display: "flex",
            fontSize: 92,
            fontWeight: 700,
            lineHeight: 1.04,
            letterSpacing: -4,
            color: "#F5F5F2",
            maxWidth: 940,
          }}
        >
          The data layer for your backend.
        </div>
      </div>
    ),
    {
      ...size,
      fonts: [{ name: "Sora", data: bold, weight: 700, style: "normal" }],
    },
  );
}
