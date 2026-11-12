import { forwardRef } from "react";
import type { LucideProps } from "lucide-react";

/**
 * The Odoo brand mark. lucide ships no brand icons, so this is the leading "o"
 * of the official Odoo logo (odoo.com), cropped out of the wordmark and inlined
 * as an SVG ring. It is typed to match `LucideIcon` (accepting `size`/`className`
 * and forwarding a ref) so it drops straight into the icon registry alongside
 * the lucide set. Unlike lucide's monochrome `currentColor` strokes, the brand
 * colour is a fixed fill.
 */
export const OdooIcon = forwardRef<SVGSVGElement, LucideProps>(
  ({ size = 24, className, ...props }, ref) => (
    <svg
      ref={ref}
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      viewBox="149 196 150 150"
      className={className}
      aria-hidden="true"
      {...props}
    >
      <path
        fill="#714B67"
        d="M224,346a75,75,0,1,1,75-75A75,75,0,0,1,224,346Zm0-31a44,44,0,1,0-44-44A44,44,0,0,0,224,315Z"
      />
    </svg>
  ),
);

OdooIcon.displayName = "OdooIcon";
