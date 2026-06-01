import { ChevronLeft, ChevronRight } from "lucide-react";

type PaginationRowProps = {
  total: number;
  limit: number;
  offset: number;
  isFetching?: boolean;
  onOffsetChange: (offset: number) => void;
};

export function PaginationRow({ total, limit, offset, isFetching, onOffsetChange }: PaginationRowProps) {
  const start = total === 0 ? 0 : offset + 1;
  const end = Math.min(total, offset + limit);
  const canPrev = offset > 0;
  const canNext = end < total;

  return (
    <section className="pagination-row">
      <span>
        {start}-{end} / {total}
      </span>
      <div className="compact-actions">
        <button
          className="secondary-action"
          disabled={!canPrev || isFetching}
          onClick={() => onOffsetChange(Math.max(0, offset - limit))}
          type="button"
        >
          <ChevronLeft size={15} />
          <span>上一页</span>
        </button>
        <button
          className="secondary-action"
          disabled={!canNext || isFetching}
          onClick={() => onOffsetChange(offset + limit)}
          type="button"
        >
          <span>下一页</span>
          <ChevronRight size={15} />
        </button>
      </div>
    </section>
  );
}
