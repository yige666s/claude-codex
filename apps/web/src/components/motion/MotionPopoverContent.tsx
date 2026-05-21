import { ComponentPropsWithoutRef, ElementRef, forwardRef } from "react";
import { motion } from "framer-motion";
import { cn } from "../../lib/utils";
import { PopoverContent } from "../ui/popover";
import { standardMotionTransition } from "./motionConfig";

type MotionPopoverContentProps = ComponentPropsWithoutRef<typeof PopoverContent> & {
  motionClassName?: string;
};

const MotionPopoverContent = forwardRef<
  ElementRef<typeof PopoverContent>,
  MotionPopoverContentProps
>(({ children, className, motionClassName, ...props }, ref) => (
  <PopoverContent ref={ref} className={cn("ui-motion-popover-content", className)} {...props}>
    <motion.div
      className={cn("ui-motion-popover-inner", motionClassName)}
      initial={{ opacity: 0, y: 4, scale: 0.98 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      exit={{ opacity: 0, y: 4, scale: 0.98 }}
      transition={standardMotionTransition}
    >
      {children}
    </motion.div>
  </PopoverContent>
));
MotionPopoverContent.displayName = "MotionPopoverContent";

export { MotionPopoverContent };
