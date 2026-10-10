import Image from "next/image";

/** A product screenshot in a quiet window frame. */
export function Frame({
  src,
  alt,
  width,
  height,
  url,
  priority,
  className = "",
}: {
  src: string;
  alt: string;
  width: number;
  height: number;
  url?: string;
  priority?: boolean;
  className?: string;
}) {
  return (
    <div className={`overflow-hidden rounded-2xl border border-border bg-card shadow-2xl shadow-black/60 ${className}`}>
      <div className="flex items-center gap-2 border-b border-border px-4 py-2.5">
        <span className="size-2.5 rounded-full bg-accent" />
        <span className="size-2.5 rounded-full bg-accent" />
        <span className="size-2.5 rounded-full bg-accent" />
        {url && <span className="ml-3 truncate font-mono text-[11px] text-muted-foreground">{url}</span>}
      </div>
      <Image src={src} alt={alt} width={width} height={height} priority={priority} sizes="(min-width: 1280px) 1200px, 100vw" className="block h-auto w-full" />
    </div>
  );
}

/** A cropped screenshot of one panel, without window chrome. */
export function Shot({
  src,
  alt,
  width,
  height,
  sizes = "(min-width: 1024px) 600px, 100vw",
  cover,
}: {
  src: string;
  alt: string;
  width: number;
  height: number;
  sizes?: string;
  cover?: boolean;
}) {
  return (
    <div className={`overflow-hidden rounded-2xl border border-border shadow-2xl shadow-black/60 ${cover ? "aspect-[16/10]" : ""}`}>
      <Image src={src} alt={alt} width={width} height={height} sizes={sizes} className={cover ? "block h-full w-full object-cover object-left-top" : "block h-auto w-full"} />
    </div>
  );
}
