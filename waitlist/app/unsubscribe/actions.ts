"use server";

import { redirect } from "next/navigation";
import { unsubscribe } from "@/lib/subscribers";
import { isUuid } from "@/lib/uuid";

export async function confirmUnsubscribe(formData: FormData) {
  const token = formData.get("token");
  if (typeof token !== "string" || !isUuid(token)) redirect("/unsubscribe");
  await unsubscribe(token);
  // The page reads the row again and shows the success state.
  redirect(`/unsubscribe?token=${token}`);
}
