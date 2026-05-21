import type { Transition } from "framer-motion";

export const standardMotionTransition: Transition = {
  duration: 0.16,
  ease: [0.2, 0, 0, 1]
};

export function prefersReducedMotion(): boolean {
  return typeof window !== "undefined"
    && typeof window.matchMedia === "function"
    && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}
