import * as React from "react";
import * as SliderPrimitive from "@radix-ui/react-slider";
import { cn } from "../../lib/utils";

const Slider = React.forwardRef<
  React.ElementRef<typeof SliderPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof SliderPrimitive.Root>
>(({ className, orientation = "horizontal", ...props }, ref) => (
  <SliderPrimitive.Root
    ref={ref}
    orientation={orientation}
    className={cn("ui-slider", orientation === "vertical" && "ui-slider-vertical", className)}
    {...props}
  >
    <SliderPrimitive.Track className="ui-slider-track">
      <SliderPrimitive.Range className="ui-slider-range" />
    </SliderPrimitive.Track>
    <SliderPrimitive.Thumb className="ui-slider-thumb" />
  </SliderPrimitive.Root>
));
Slider.displayName = SliderPrimitive.Root.displayName;

export { Slider };
