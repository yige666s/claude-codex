import { HTMLMotionProps, motion } from "framer-motion";
import { cn } from "../../lib/utils";
import { prefersReducedMotion, standardMotionTransition } from "./motionConfig";

type MotionListItemProps = HTMLMotionProps<"div">;

export function MotionListItem({ className, children, ...props }: MotionListItemProps) {
  const reduceMotion = prefersReducedMotion();
  return (
    <motion.div
      className={cn("motion-list-item", className)}
      initial={reduceMotion ? false : { opacity: 0, scale: 0.98 }}
      animate={reduceMotion ? undefined : { opacity: 1, scale: 1 }}
      exit={reduceMotion ? undefined : { opacity: 0, scale: 0.98 }}
      transition={standardMotionTransition}
      {...props}
    >
      {children}
    </motion.div>
  );
}
