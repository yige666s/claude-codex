import type { RuntimeEvent } from "../types";

export type ParsedSSE = {
  id?: string;
  event: string;
  data: RuntimeEvent;
};

export async function readSSEStream(response: Response, onEvent: (event: ParsedSSE) => void): Promise<void> {
  if (!response.body) return;
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    buffer = buffer.replace(/\r\n/g, "\n");
    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      const chunk = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      const parsed = parseSSEChunk(chunk);
      if (parsed) onEvent(parsed);
      boundary = buffer.indexOf("\n\n");
    }
  }
  const tail = buffer.trim();
  if (tail) {
    const parsed = parseSSEChunk(tail);
    if (parsed) onEvent(parsed);
  }
}

export function parseSSEChunk(chunk: string): ParsedSSE | null {
  const lines = chunk.split(/\r?\n/);
  let event = "message";
  let id = "";
  const data: string[] = [];
  for (const line of lines) {
    if (line.startsWith("event:")) event = line.slice(6).trim();
    if (line.startsWith("id:")) id = line.slice(3).trim();
    if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
  }
  if (!data.length) return null;
  try {
    return { id: id || undefined, event, data: JSON.parse(data.join("\n")) as RuntimeEvent };
  } catch {
    return null;
  }
}
