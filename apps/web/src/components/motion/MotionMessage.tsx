import { HTMLMotionProps, motion } from "framer-motion";
import { cn } from "../../lib/utils";
import { prefersReducedMotion, standardMotionTransition } from "./motionConfig";

type MotionMessageProps = HTMLMotionProps<"article">;

export function MotionMessage({ className, children, ...props }: MotionMessageProps) {
  const reduceMotion = prefersReducedMotion();
  return (
    <motion.article
      className={cn("motion-message", className)}
      initial={reduceMotion ? false : { opacity: 0, y: 6 }}
      animate={reduceMotion ? undefined : { opacity: 1, y: 0 }}
      exit={reduceMotion ? undefined : { opacity: 0, y: 6 }}
      transition={standardMotionTransition}
      {...props}
    >
      {children}
    </motion.article>
  );
}
