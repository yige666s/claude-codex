import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/utils";

const buttonVariants = cva("ui-button", {
  variants: {
    variant: {
      default: "ui-button-default",
      primary: "ui-button-primary",
      secondary: "ui-button-secondary",
      destructive: "ui-button-destructive",
      outline: "ui-button-outline",
      ghost: "ui-button-ghost",
      link: "ui-button-link"
    },
    size: {
      default: "ui-button-md",
      xs: "ui-button-xs",
      sm: "ui-button-sm",
      lg: "ui-button-lg",
      icon: "ui-button-icon",
      "icon-sm": "ui-button-icon-sm",
      "icon-lg": "ui-button-icon-lg"
    }
  },
  defaultVariants: {
    variant: "default",
    size: "default"
  }
});

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return <Comp className={cn(buttonVariants({ variant, size, className }))} ref={ref} {...props} />;
  }
);
Button.displayName = "Button";

export { Button, buttonVariants };
