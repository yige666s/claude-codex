import { MessageCircle, Search, X } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "../../../components/ui/dialog";
import { Input } from "../../../components/ui/input";
import type { MessageSearchResult } from "../../../types";

type GlobalSearchDialogProps = {
  open: boolean;
  query: string;
  loading: boolean;
  error: string;
  results: MessageSearchResult[];
  onOpenChange: (open: boolean) => void;
  onQueryChange: (value: string) => void;
  onOpenResult: (result: MessageSearchResult) => void;
  formatTime: (value?: string) => string;
};

export function GlobalSearchDialog({
  open,
  query,
  loading,
  error,
  results,
  onOpenChange,
  onQueryChange,
  onOpenResult,
  formatTime
}: GlobalSearchDialogProps) {
  const hasQuery = Boolean(query.trim());

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="global-search-modal"
        hideClose
      >
        <DialogTitle className="sr-only">Search across all sessions</DialogTitle>
        <DialogDescription className="sr-only">Search messages across all saved conversations.</DialogDescription>
        <div className="global-search-input">
          <Search size={18} />
          <Input
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            placeholder="Search across all sessions"
            aria-label="Search across all sessions"
            autoFocus
          />
          <Button variant="ghost" size="icon" onClick={() => onOpenChange(false)} title="Close search" aria-label="Close search">
            <X size={20} />
          </Button>
        </div>
        <div className="global-search-results">
          {!hasQuery && <div className="global-search-empty">Type to search conversations</div>}
          {hasQuery && loading && <div className="global-search-empty">Searching...</div>}
          {hasQuery && !loading && error && <div className="global-search-empty error-text">{error}</div>}
          {hasQuery && !loading && !error && !results.length && <div className="global-search-empty">No results</div>}
          {hasQuery && !loading && !error && results.map((result) => (
            <Button key={`${result.session_id}-${result.message_index}-${result.created_at}`} className="global-search-result" onClick={() => onOpenResult(result)}>
              <MessageCircle size={19} />
              <span>
                <strong>{result.session_title || result.session_id}</strong>
                <small>{result.snippet || result.content || ""}</small>
              </span>
              <time>{formatTime(result.created_at)}</time>
            </Button>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
