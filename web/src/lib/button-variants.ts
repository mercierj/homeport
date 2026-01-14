import { cva } from "class-variance-authority";

export const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-primary-hover shadow-sm",
        primary: "bg-primary text-primary-foreground hover:bg-primary-hover shadow-sm",
        secondary: "bg-secondary text-secondary-foreground hover:bg-secondary-hover shadow-sm",
        accent: "bg-accent text-accent-foreground hover:bg-accent-hover shadow-sm",
        success: "bg-success text-success-foreground hover:bg-success/90 shadow-sm",
        warning: "bg-warning text-warning-foreground hover:bg-warning/90 shadow-sm",
        error: "bg-error text-error-foreground hover:bg-error/90 shadow-sm",
        info: "bg-info text-info-foreground hover:bg-info/90 shadow-sm",
        outline: "border border-input bg-background hover:bg-accent hover:text-accent-foreground",
        ghost: "hover:bg-accent hover:text-accent-foreground",
        link: "text-primary underline-offset-4 hover:underline",

        /* Cloud provider variants for migration UI */
        aws: "bg-cloud-aws text-cloud-aws-foreground hover:bg-cloud-aws/90 shadow-sm",
        gcp: "bg-cloud-gcp text-cloud-gcp-foreground hover:bg-cloud-gcp/90 shadow-sm",
        azure: "bg-cloud-azure text-cloud-azure-foreground hover:bg-cloud-azure/90 shadow-sm",
        freedom: "bg-freedom text-freedom-foreground hover:bg-freedom/90 shadow-sm shadow-glow-success",
      },
      size: {
        default: "h-10 px-4 py-2",
        sm: "h-9 rounded-md px-3 text-xs",
        lg: "h-11 rounded-md px-8 text-base",
        xl: "h-14 rounded-lg px-10 text-lg",
        icon: "h-10 w-10",
        "icon-sm": "h-8 w-8",
        "icon-lg": "h-12 w-12",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
);
