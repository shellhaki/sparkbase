"use client";

import { useEffect, useState } from "react";

// "Hello" in many languages, starting with the ones spoken around Lagos.
const HELLOS: Array<{ word: string; lang: string; name: string }> = [
  { word: "Hello", lang: "en", name: "English" },
  { word: "Ẹ n lẹ", lang: "yo", name: "Yorùbá" },
  { word: "Sannu", lang: "ha", name: "Hausa" },
  { word: "Ndewo", lang: "ig", name: "Igbo" },
  { word: "How far", lang: "pcm", name: "Naijá" },
  { word: "Habari", lang: "sw", name: "Kiswahili" },
  { word: "Bonjour", lang: "fr", name: "Français" },
  { word: "Hola", lang: "es", name: "Español" },
  { word: "Olá", lang: "pt", name: "Português" },
  { word: "Ciao", lang: "it", name: "Italiano" },
  { word: "Hallo", lang: "de", name: "Deutsch" },
  { word: "Merhaba", lang: "tr", name: "Türkçe" },
  { word: "مرحبا", lang: "ar", name: "العربية" },
  { word: "नमस्ते", lang: "hi", name: "हिन्दी" },
  { word: "こんにちは", lang: "ja", name: "日本語" },
  { word: "안녕하세요", lang: "ko", name: "한국어" },
  { word: "你好", lang: "zh", name: "中文" },
  { word: "Привет", lang: "ru", name: "Русский" },
  { word: "Sawubona", lang: "zu", name: "isiZulu" },
  { word: "Akwaaba", lang: "ak", name: "Akan" },
];

/** "Hello, Ada." cycling through languages with a soft blur swap. */
export function Greeting({ name }: { name: string }) {
  const [i, setI] = useState(0);
  const [state, setState] = useState<"in" | "out">("in");

  useEffect(() => {
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const every = reduce ? 6000 : 3200;
    let swap: ReturnType<typeof setTimeout>;
    const t = setInterval(() => {
      setState("out");
      swap = setTimeout(() => {
        setI((n) => (n + 1) % HELLOS.length);
        setState("in");
      }, 380);
    }, every);
    return () => {
      clearInterval(t);
      clearTimeout(swap);
    };
  }, []);

  const h = HELLOS[i];
  return (
    <div>
      <h1 className="font-display text-4xl font-semibold tracking-tight sm:text-5xl">
        <span lang={h.lang} dir={h.lang === "ar" ? "rtl" : undefined} className="greet-word text-brand" data-state={state}>
          {h.word}
        </span>
        <span className="text-foreground">, {name}.</span>
      </h1>
      <p className="mt-2 h-5 font-mono text-xs tracking-wide text-muted-foreground uppercase">
        <span className="greet-word" data-state={state}>
          {h.name}
        </span>
      </p>
    </div>
  );
}
