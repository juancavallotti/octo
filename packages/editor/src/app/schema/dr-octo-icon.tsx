import { forwardRef } from "react";
import type { LucideProps } from "lucide-react";

/**
 * Dr. Octo's mark: the octopus-headed doctor the platform's agent is named for.
 *
 * Drawn rather than borrowed — lucide's generic robot said "an AI is here",
 * which is the one thing about this agent that needs no saying, and said nothing
 * about which one. Typed as a LucideProps icon (size, className, forwarded ref)
 * so it drops in wherever the robot stood.
 *
 * It lives here, beside the other brand marks, because the icon registry is what
 * the integration icon picker offers: an octopus that only the agent's own chat
 * button could use would not be choosable for the integration that IS the agent.
 *
 * Strokes use `currentColor` at the same 12/256 weight as the source drawing, so
 * it inherits its colour and reads correctly on the sky-600 launcher and in a
 * grey nav row alike.
 */
export const DrOctoIcon = forwardRef<SVGSVGElement, LucideProps>(
  ({ size = 24, className, ...props }, ref) => (
    <svg
      ref={ref}
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      viewBox="0 0 256 256"
      fill="none"
      className={className}
      aria-hidden="true"
      {...props}
    >
      <g
        stroke="currentColor"
        strokeWidth={12}
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="M128 34V22" />
        <circle cx="128" cy="14" r="7" />
        <rect x="58" y="46" width="140" height="112" rx="34" />
        <path d="M58 78H46c-8 0-14 6-14 14v20c0 8 6 14 14 14h12" />
        <path d="M198 78h12c8 0 14 6 14 14v20c0 8-6 14-14 14h-12" />
        <path d="M86 86l22 6" />
        <path d="M170 86l-22 6" />
        <rect x="84" y="98" width="22" height="26" rx="7" />
        <rect x="150" y="98" width="22" height="26" rx="7" />
        <path d="M108 134c7 7 13 10 20 10s13-3 20-10" />
        <path d="M82 150c-4 20-18 37-34 43-12 5-25 1-29-9-4-9 0-19 9-23 7-3 15 0 18 7" />
        <path d="M102 154c-1 25-8 47-21 58-9 8-20 7-25-1-5-8-2-18 6-23 5-3 11-3 16 1" />
        <path d="M116 157c0 30-3 54-10 68" />
        <path d="M140 157c0 30 3 54 10 68" />
        <path d="M154 154c1 25 8 47 21 58 9 8 20 7 25-1 5-8 2-18-6-23-5-3-11-3-16 1" />
        <path d="M174 150c4 20 18 37 34 43 12 5 25 1 29-9 4-9 0-19-9-23-7-3-15 0-18 7" />
      </g>
    </svg>
  ),
);

DrOctoIcon.displayName = "DrOctoIcon";
