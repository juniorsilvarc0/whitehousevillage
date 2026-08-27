import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const etiqueta = cva(
  "inline-flex shrink-0 items-center gap-1.5 rounded-full px-2.5 py-0.5 text-[0.7rem] font-medium leading-5 [&_svg]:size-3",
  {
    variants: {
      variant: {
        neutral: "bg-secondary text-secondary-foreground",
        accent: "bg-accent text-accent-foreground",
        brand: "bg-brand-gradient text-white",
        outline: "border border-border text-muted-foreground",
        destructive: "bg-destructive/12 text-destructive",
      },
    },
    defaultVariants: { variant: "neutral" },
  },
);

export type BadgeProps = React.ComponentProps<"span"> & VariantProps<typeof etiqueta>;

export function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(etiqueta({ variant }), className)} {...props} />;
}
