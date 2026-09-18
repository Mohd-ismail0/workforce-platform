import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * Merge conditional class names, letting later Tailwind utilities win over earlier ones of the
 * same property. Without the merge, a caller passing `className="p-0"` to a component whose
 * base sets `px-3 py-2` produces both and the base wins by source order, which is the reverse
 * of what every caller expects.
 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
