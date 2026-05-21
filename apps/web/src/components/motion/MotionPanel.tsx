import { HTMLMotionProps, motion } from "framer-motion";
import { cn } from "../../lib/utils";
import { prefersReducedMotion, standardMotionTransition } from "./motionConfig";

type MotionPanelProps = HTMLMotionProps<"div">;

export function MotionPanel({ className, children, ...props }: MotionPanelProps) {
  const reduceMotion = prefersReducedMotion();
  return (
    <motion.div
      className={cn("motion-panel", className)}
      initial={reduceMotion ? false : { opacity: 0, y: 8 }}
      animate={reduceMotion ? undefined : { opacity: 1, y: 0 }}
      exit={reduceMotion ? undefined : { opacity: 0, y: 8 }}
      transition={standardMotionTransition}
      {...props}
    >
      {children}
    </motion.div>
  );
}
