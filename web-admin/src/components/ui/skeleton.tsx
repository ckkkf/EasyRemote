import { cn } from '../../lib/utils';

/** shadcn/ui Skeleton：加载占位（不写死宽度，交给调用方）。 */
function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('animate-pulse rounded-md bg-muted', className)} {...props} />;
}

export { Skeleton };
