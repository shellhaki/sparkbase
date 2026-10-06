import Image from "next/image";
import mark from "@/public/brand/sparkbase-mark-128.png";

export function LogoMark({ size = 24 }: { size?: number }) {
  return <Image src={mark} alt="" width={size} height={size} priority />;
}

/** Two-tone wordmark: "Spark" in the text color, "base" in brand blue. */
export function Wordmark({ className = "" }: { className?: string }) {
  return (
    <span className={`font-brand font-bold tracking-tight ${className}`}>
      <span className="text-foreground">Spark</span>
      <span className="text-brand">base</span>
    </span>
  );
}
