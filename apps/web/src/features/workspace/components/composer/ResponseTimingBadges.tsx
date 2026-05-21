type ResponseTimingBadgesProps = {
  timing: { ttftMs?: number; totalMs?: number } | null;
  formatNumber: (value: number) => string;
};

export function ResponseTimingBadges({ timing, formatNumber }: ResponseTimingBadgesProps) {
  if (!timing) return null;
  return (
    <div className="response-metrics" aria-live="polite">
      <span>TTFT {formatNumber(timing.ttftMs || 0)} ms</span>
      {timing.totalMs !== undefined && <span>Total {formatNumber(timing.totalMs)} ms</span>}
    </div>
  );
}
