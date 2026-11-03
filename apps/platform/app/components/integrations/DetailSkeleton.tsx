import { SkeletonCard, SkeletonLine } from "@/app/components/ui/Skeleton";

/** The detail pane while the integration it is about is still loading. */
export default function DetailSkeleton() {
  return (
    <div aria-busy className="flex h-full flex-col">
      <div className="flex items-center gap-2 px-4 py-3">
        <span className="h-7 w-7 shrink-0 animate-pulse rounded-md bg-black/[0.06] dark:bg-white/[0.08]" />
        <SkeletonLine width="12rem" className="h-4" />
      </div>
      <div className="grid grid-cols-1 gap-3 p-3 lg:grid-cols-2">
        <SkeletonCard title="Details" lines={5} />
        <OperatingSkeletons />
      </div>
    </div>
  );
}

/** The version-scoped cards while the pane's first read is out. */
export function OperatingSkeletons() {
  return (
    <>
      <SkeletonCard title="Deployments" lines={3} />
      <SkeletonCard title="Resources" lines={4} />
      <SkeletonCard title="Env" lines={2} />
    </>
  );
}
